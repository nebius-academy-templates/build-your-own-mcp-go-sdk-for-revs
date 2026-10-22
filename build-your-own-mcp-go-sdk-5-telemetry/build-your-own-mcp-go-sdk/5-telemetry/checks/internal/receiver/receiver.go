// Package receiver collects OTLP spans for the black-box checks and trace viewer.
package receiver

import (
	"encoding/hex"
	"io"
	"net/http"
	"sort"
	"sync"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type Record struct {
	Span     *tracepb.Span
	Resource map[string]any
}

type Receiver struct {
	mu      sync.Mutex
	records map[string]Record
	Posts   int
}

func Attributes(kvs []*common.KeyValue) map[string]any {
	result := make(map[string]any)
	for _, kv := range kvs {
		switch v := kv.Value.Value.(type) {
		case *common.AnyValue_StringValue:
			result[kv.Key] = v.StringValue
		case *common.AnyValue_IntValue:
			result[kv.Key] = v.IntValue
		case *common.AnyValue_BoolValue:
			result[kv.Key] = v.BoolValue
		case *common.AnyValue_DoubleValue:
			result[kv.Key] = v.DoubleValue
		default:
			result[kv.Key] = kv.Value.String()
		}
	}
	return result
}

func ID(b []byte) string { return hex.EncodeToString(b) }

func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost || req.URL.Path != "/v1/traces" {
		http.NotFound(w, req)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 16<<20))
	var batch collector.ExportTraceServiceRequest
	if err != nil || proto.Unmarshal(data, &batch) != nil {
		http.Error(w, "invalid OTLP protobuf", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	if r.records == nil {
		r.records = make(map[string]Record)
	}
	r.Posts++
	for _, resource := range batch.ResourceSpans {
		attrs := Attributes(resource.GetResource().GetAttributes())
		for _, scope := range resource.ScopeSpans {
			for _, span := range scope.Spans {
				r.records[ID(span.TraceId)+":"+ID(span.SpanId)] = Record{span, attrs}
			}
		}
	}
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK) // An empty protobuf response is valid.
}

func (r *Receiver) Snapshot() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Span.StartTimeUnixNano < result[j].Span.StartTimeUnixNano })
	return result
}
