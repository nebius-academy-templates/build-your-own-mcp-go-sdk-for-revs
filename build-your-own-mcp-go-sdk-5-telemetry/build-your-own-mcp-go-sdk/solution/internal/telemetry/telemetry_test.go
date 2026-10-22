package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupDisabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	enabled, stop, err := Setup(context.Background(), "test")
	if err != nil || enabled {
		t.Fatalf("Setup disabled: enabled=%v error=%v", enabled, err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestSetupFlushesOnFreshContext(t *testing.T) {
	oldProvider, oldPropagator, oldError := otel.GetTracerProvider(), otel.GetTextMapPropagator(), otel.GetErrorHandler()
	defer otel.SetTracerProvider(oldProvider)
	defer otel.SetTextMapPropagator(oldPropagator)
	defer otel.SetErrorHandler(oldError)
	var posts atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("unexpected OTLP request %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		posts.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer receiver.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", receiver.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "30000")
	ctx, cancel := context.WithCancel(context.Background())
	enabled, stop, err := Setup(ctx, "test")
	if err != nil || !enabled {
		t.Fatalf("Setup enabled: %v %v", enabled, err)
	}
	_, span := otel.Tracer("test").Start(ctx, "test-span")
	span.End()
	cancel()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Errorf("flush posts=%d, want 1", posts.Load())
	}
}

func TestReaderCounts(t *testing.T) {
	for _, tc := range []struct {
		name, text   string
		bytes, lines int64
	}{{"empty", "", 0, 0}, {"terminated", "a\r\nb\n", 5, 2}, {"unterminated", "a\nb", 3, 2}, {"blank", "\n", 1, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			old := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			defer otel.SetTracerProvider(old)
			defer provider.Shutdown(context.Background())
			ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")
			r := Reader(ctx, io.NopCloser(strings.NewReader(tc.text)))
			if _, err := io.ReadAll(r); err != nil {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			parent.End()
			spans := recorder.Ended()
			if len(spans) != 2 {
				t.Fatalf("got %d spans, want reader and parent", len(spans))
			}
			if spans[0].Name() != "logtriage.read" || spans[0].Parent().SpanID() != parent.SpanContext().SpanID() {
				t.Fatal("reader must be the parent's child")
			}
			attrs := map[string]int64{}
			for _, kv := range spans[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value.AsInt64()
			}
			if attrs["logtriage.bytes"] != tc.bytes || attrs["logtriage.lines"] != tc.lines || attrs["logtriage.files"] != 1 {
				t.Errorf("unexpected counters: %v", attrs)
			}
		})
	}
}
