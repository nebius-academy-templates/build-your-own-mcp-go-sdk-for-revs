package checks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestT4PrivacyAndCaller(t *testing.T) {
	s := start(t, "http", true, nil)
	s.call("search_logs", map[string]any{"pattern": "PRIVATE_ARGUMENT_MARKER"}, nil)
	s.call("read_lines", map[string]any{"path": "MISSING_PRIVATE_PATH_MARKER", "line": 1}, nil)
	s.rpc("tools/list", nil, nil, "p5-secret-viewer")
	rows := s.stop("signal")
	if len(rows) == 0 {
		t.Fatal("telemetry export missing")
	}
	for _, r := range rows {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"p5-secret-writer", "p5-secret-viewer", "PRIVATE_ARGUMENT_MARKER", "MISSING_PRIVATE_PATH_MARKER", "request 1 failed", "Authorization"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("span leaks forbidden data %q", secret)
			}
		}
	}
	equal(t, attrs(find(t, rows, "tools/call search_logs"))["enduser.id"], "operator")
	equal(t, attrs(find(t, rows, "tools/list"))["enduser.id"], "observer")
}

// A URI the server refuses is caller input, whatever its shape. Only a URI the
// server served is protocol metadata.
func TestT4ResourceURIs(t *testing.T) {
	s := start(t, "stdio", true, nil)
	s.rpc("resources/read", map[string]any{"uri": "triage://reports/private-report-marker"}, nil, "")
	s.rpc("resources/read", map[string]any{"uri": "triage://private-uri-marker"}, nil, "")
	s.rpc("resources/read", map[string]any{"uri": "triage://formats"}, nil, "")
	served := []any{}
	for _, r := range s.stop("eof") {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"private-report-marker", "private-uri-marker"} {
			if strings.Contains(string(raw), marker) {
				t.Errorf("span leaks the refused URI %q", marker)
			}
		}
		if r.Span.Name == "resources/read" {
			if uri, ok := attrs(r)["mcp.resource.uri"]; ok {
				served = append(served, uri)
			}
		}
	}
	equalWhy(t, served, []any{"triage://formats"}, "only the served resource's URI belongs on a resources/read span")
}
