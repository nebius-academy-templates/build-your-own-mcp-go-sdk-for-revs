// Package telemetry configures opt-in OTLP tracing and request-local read counters.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// Setup returns whether export is enabled and a bounded flush operation.
// Error messages intentionally omit exporter configuration and credentials.
func Setup(ctx context.Context, version string) (bool, func() error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return false, func() error { return nil }, nil
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { fmt.Fprintln(os.Stderr, "telemetry: export failed") }))
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf))
	if err != nil {
		return false, nil, errors.New("telemetry: exporter setup failed")
	}
	name := os.Getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "log-triage"
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(name), semconv.ServiceVersion(version))),
		sdktrace.WithoutPanicRecording(),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return true, func() error {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(flush); err != nil {
			return errors.New("telemetry: shutdown export failed")
		}
		return nil
	}, nil
}
