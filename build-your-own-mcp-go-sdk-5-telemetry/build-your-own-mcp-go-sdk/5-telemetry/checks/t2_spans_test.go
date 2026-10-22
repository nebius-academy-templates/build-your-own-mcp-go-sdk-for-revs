package checks

import (
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/5-telemetry/checks/internal/receiver"
	"strings"
	"testing"
)

func TestT2RequestsAndErrors(t *testing.T) {
	s := start(t, "stdio", true, nil)
	s.rpc("tools/list", nil, nil, "")
	s.call("list_logs", map[string]any{"path": "../forbidden"}, nil)
	s.rpc("prompts/get", map[string]any{"name": "triage", "arguments": map[string]any{}}, nil, "")
	s.rpc("resources/read", map[string]any{"uri": "triage://reports"}, nil, "")
	rows := s.stop("eof")
	for _, name := range []string{"tools/list", "tools/call list_logs", "prompts/get triage", "resources/read"} {
		r := find(t, rows, name)
		equal(t, int32(r.Span.Kind), int32(2))
		equal(t, attrs(r)["mcp.method.name"], strings.Split(name, " ")[0])
		count := 0
		for _, other := range rows {
			if other.Span.Name == name {
				count++
			}
		}
		equal(t, count, 1)
	}
	r := find(t, rows, "tools/call list_logs")
	equal(t, attrs(r)["error.type"], "tool_error")
	equal(t, int32(r.Span.GetStatus().GetCode()), int32(2))
	equal(t, attrs(r)["gen_ai.operation.name"], "execute_tool")
	equal(t, attrs(r)["gen_ai.tool.name"], "list_logs")
	if !strings.Contains(s.stderr.text(), "trace_id="+receiver.ID(r.Span.TraceId)) {
		t.Error("debug tool log must contain its server span's trace_id")
	}
	p := find(t, rows, "prompts/get triage")
	equal(t, attrs(p)["gen_ai.prompt.name"], "triage")
	if p.Span.GetStatus().GetCode() == 2 {
		t.Error("invalid prompt arguments are not a server failure")
	}
	equal(t, attrs(find(t, rows, "resources/read"))["mcp.resource.uri"], "triage://reports")
}
