package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	r := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r), sdktrace.WithoutPanicRecording())
	old, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(p)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(prop)
		if err := p.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestTracingClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int64
		isError bool
		want    string
	}{
		{"success", 0, false, ""}, {"refusal", 0, true, "tool_error"}, {"invalid", -32602, false, ""}, {"missing", -32002, false, ""}, {"fault", -32603, false, "-32603"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := recordSpans(t)
			next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
				if tc.code != 0 {
					return nil, &jsonrpc.Error{Code: tc.code, Message: "private-marker"}
				}
				return &mcp.CallToolResult{IsError: tc.isError, Content: []mcp.Content{&mcp.TextContent{Text: "private-marker"}}}, nil
			}
			_, _ = traceRequests(next)(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "list_logs", Arguments: json.RawMessage(`{"path":"private-marker"}`)}})
			spans := r.Ended()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}
			span := spans[0]
			typ := ""
			for _, kv := range span.Attributes() {
				if kv.Key == "error.type" {
					typ = kv.Value.AsString()
				}
			}
			if typ != tc.want {
				t.Errorf("error.type=%q want %q", typ, tc.want)
			}
			if (span.Status().Code == codes.Error) != (tc.want != "") {
				t.Errorf("wrong status %v", span.Status())
			}
			if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "private-marker") {
				t.Error("payload leaked into telemetry")
			}
		})
	}
}

func TestTracingContextAndNotifications(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parent any
		count  int
	}{
		{"sampled", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", 1},
		{"unsampled", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00", 0},
		{"invalid", "not-a-trace", 1}, {"non-string", 42, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := recordSpans(t)
			p := &mcp.CallToolParamsRaw{Name: "list_logs"}
			p.SetMeta(map[string]any{"traceparent": tc.parent, "tracestate": "vendor=value"})
			called := false
			next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				called = true
				if callerOf(ctx).Name != "tester" {
					t.Error("caller context lost")
				}
				return &mcp.CallToolResult{}, nil
			}
			_, err := traceRequests(next)(WithCaller(context.Background(), Caller{Name: "tester"}), "tools/call", &mcp.CallToolRequest{Params: p})
			if err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("handler not called")
			}
			spans := r.Ended()
			if len(spans) != tc.count {
				t.Fatalf("spans=%d want=%d", len(spans), tc.count)
			}
			if tc.name == "sampled" {
				if spans[0].Parent().SpanID().String() != "00f067aa0ba902b7" || spans[0].SpanContext().TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
					t.Fatal("wrong remote parent")
				}
			}
			if tc.name == "invalid" || tc.name == "non-string" {
				if spans[0].Parent().IsValid() {
					t.Error("invalid parent must create a root")
				}
			}
		})
	}
	t.Run("notification", func(t *testing.T) {
		r := recordSpans(t)
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil }
		_, _ = traceRequests(next)(context.Background(), "notifications/initialized", nil)
		if len(r.Ended()) != 0 {
			t.Fatal("notification created a request span")
		}
	})
}

func TestTracingMaskedFault(t *testing.T) {
	r := recordSpans(t)
	root, err := paths.Resolve(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(t.TempDir(), "reports")
	if err := os.WriteFile(reports, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &testServer{server: NewServer(Config{Root: root, Reports: reports, ScanTTL: time.Minute, Telemetry: true})}
	s.reconnect(t)
	got := s.refusal(t, "save_finding", map[string]any{"title": "private-marker-title", "severity": "low", "summary": "private-marker-summary", "evidence": []any{map[string]any{"file": "app/api.log", "line": 1, "note": "private-marker-note"}}})
	if got != unexpectedText {
		t.Errorf("masked result=%q, want %q", got, unexpectedText)
	}
	found := false
	for _, span := range r.Ended() {
		if span.Name() == "tools/call save_finding" {
			found = true
			if span.Status().Code != codes.Error {
				t.Error("masked fault must be marked as failed")
			}
			if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "private-marker") {
				t.Error("masked fault leaked input")
			}
		}
	}
	if !found {
		t.Fatal("missing save request span")
	}
}

func TestTracingTypedNilResult(t *testing.T) {
	r := recordSpans(t)
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return (*mcp.CallToolResult)(nil), nil
	}
	_, err := traceRequests(next)(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "list_logs"}})
	if err != nil || len(r.Ended()) != 1 {
		t.Fatalf("typed nil result: err=%v spans=%d", err, len(r.Ended()))
	}
}

func TestTracingOptionalParams(t *testing.T) {
	r := recordSpans(t)
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil }
	_, err := traceRequests(next)(context.Background(), "ping", &mcp.ServerRequest[*mcp.PingParams]{})
	if err != nil || len(r.Ended()) != 1 {
		t.Fatalf("optional params: err=%v spans=%d", err, len(r.Ended()))
	}
	if r.Ended()[0].Name() != "ping" {
		t.Error("parameter-free request must retain its method span")
	}
}
