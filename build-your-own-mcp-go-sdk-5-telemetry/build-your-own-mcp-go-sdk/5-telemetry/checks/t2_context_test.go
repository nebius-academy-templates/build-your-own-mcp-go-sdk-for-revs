package checks

import (
	"encoding/json"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/5-telemetry/checks/internal/receiver"
	"testing"
	"time"
)

const parentTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
const parentSpan = "00f067aa0ba902b7"

// Legacy MCP permits requests with no params. The server must retain this
// behavior while its receiving middleware extracts optional metadata.
func TestT2OptionalParams(t *testing.T) {
	s := start(t, "stdio", true, nil)
	send := func(frame map[string]any) {
		t.Helper()
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.in.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	reply := func() map[string]any {
		t.Helper()
		select {
		case raw, open := <-s.lines:
			if !open {
				t.Fatalf("server closed before replying to a valid request with optional params: %s", s.stderr.text())
			}
			var frame map[string]any
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatalf("invalid JSON-RPC response: %q", raw)
			}
			return frame
		case <-time.After(5 * time.Second):
			t.Fatal("no reply to a valid request with optional params")
			return nil
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]any{"name": "telemetry-checks", "version": "1"}, "capabilities": map[string]any{}}})
	if init := reply(); init["error"] != nil {
		t.Fatalf("legacy initialization failed: %v", init)
	}
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "ping"})
	if result := reply(); result["result"] == nil || result["error"] != nil {
		t.Fatalf("parameter-free ping must succeed: %v", result)
	}
	rows := s.stop("eof")
	equal(t, int32(find(t, rows, "ping").Span.Kind), int32(2))
	for _, row := range rows {
		if row.Span.Name == "notifications/initialized" {
			t.Error("notification must not create a span")
		}
	}
}

func TestT2Parent(t *testing.T) {
	for _, transport := range []string{"stdio", "http"} {
		t.Run(transport, func(t *testing.T) {
			s := start(t, transport, true, nil)
			s.call("list_logs", map[string]any{}, map[string]any{"traceparent": "00-" + parentTrace + "-" + parentSpan + "-01", "tracestate": "vendor=value"})
			r := find(t, s.stop("signal"), "tools/call list_logs")
			equal(t, receiver.ID(r.Span.TraceId), parentTrace)
			equal(t, receiver.ID(r.Span.ParentSpanId), parentSpan)
			equal(t, r.Span.TraceState, "vendor=value")
		})
	}
}

func TestT2Unsampled(t *testing.T) {
	s := start(t, "stdio", true, nil)
	s.call("list_logs", map[string]any{}, map[string]any{"traceparent": "00-" + parentTrace + "-" + parentSpan + "-00"})
	s.call("list_logs", map[string]any{}, nil)
	own := 0
	for _, r := range s.stop("eof") {
		if receiver.ID(r.Span.TraceId) == parentTrace {
			t.Error("unsampled parent must not export its descendants")
		} else if r.Span.Name == "tools/call list_logs" {
			own++
		}
	}
	// The second call has no caller context, so it starts its own sampled trace.
	equalWhy(t, own, 1, "the call without caller context must export its own span")
}

func TestT2ScanIsNotTrace(t *testing.T) {
	s := start(t, "stdio", true, nil)
	scan := structured(t, s.call("summarize_logs", map[string]any{"min_level": "any"}, nil))
	groups := scan["groups"].([]any)
	group := groups[0].(map[string]any)
	args := map[string]any{"scan_id": scan["scan_id"], "group_id": group["group_id"]}
	s.call("group_lines", args, nil)
	s.call("group_lines", args, nil)
	seen := map[string]bool{}
	calls := 0
	for _, r := range s.stop("eof") {
		if r.Span.Kind == 2 {
			calls++
			seen[receiver.ID(r.Span.TraceId)] = true
		}
	}
	equalWhy(t, calls, 3, "each of the three calls must export its own SERVER span")
	equal(t, len(seen), 3)
}
