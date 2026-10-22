package telemetry

import (
	"bytes"
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"io"
)

// Reader measures one decompressed reader's lifetime. It exports no file path.
// Format detection and full reads are separate operations, as in the parser.
func Reader(ctx context.Context, source io.ReadCloser) io.ReadCloser {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return source
	}
	_, span := otel.Tracer("log-triage").Start(ctx, "logtriage.read", trace.WithSpanKind(trace.SpanKindInternal))
	return &countedReader{source: source, span: span}
}

type countedReader struct {
	source          io.ReadCloser
	span            trace.Span
	bytes, lines    int64
	partial, closed bool
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		r.bytes += int64(n)
		r.lines += int64(bytes.Count(p[:n], []byte{'\n'}))
		r.partial = p[n-1] != '\n'
	}
	if err != nil && err != io.EOF {
		r.span.SetAttributes(attribute.String("error.type", "read_error"))
		r.span.SetStatus(codes.Error, "")
	}
	return n, err
}
func (r *countedReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.source.Close()
	if r.partial {
		r.lines++
	}
	if err != nil {
		r.span.SetAttributes(attribute.String("error.type", "read_error"))
		r.span.SetStatus(codes.Error, "")
	}
	r.span.SetAttributes(attribute.Int("logtriage.files", 1), attribute.Int64("logtriage.bytes", r.bytes), attribute.Int64("logtriage.lines", r.lines))
	r.span.End()
	return err
}
