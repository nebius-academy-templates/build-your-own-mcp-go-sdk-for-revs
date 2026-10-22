package checks

import "testing"

func TestT1Disabled(t *testing.T) {
	s := start(t, "stdio", false, nil)
	s.call("list_logs", map[string]any{}, nil)
	if rows := s.stop("eof"); len(rows) != 0 {
		t.Fatalf("telemetry must be off without an endpoint: %d spans", len(rows))
	}
}

func TestT1ExportAndShutdown(t *testing.T) {
	for _, tc := range []struct{ transport, stop string }{{"stdio", "eof"}, {"stdio", "signal"}, {"http", "signal"}} {
		t.Run(tc.transport+"-"+tc.stop, func(t *testing.T) {
			s := start(t, tc.transport, true, map[string]string{"OTEL_SERVICE_NAME": "p5-test-service"})
			s.call("list_logs", map[string]any{}, nil)
			r := find(t, s.stop(tc.stop), "tools/call list_logs")
			equal(t, r.Resource["service.name"], "p5-test-service")
			equal(t, r.Resource["service.version"], "0.5.0")
		})
	}
}
