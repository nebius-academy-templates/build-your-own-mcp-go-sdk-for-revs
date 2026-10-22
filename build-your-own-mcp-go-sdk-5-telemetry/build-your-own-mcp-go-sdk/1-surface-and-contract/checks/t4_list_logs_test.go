package checks

// Task 4: list_logs to the contract: schema, annotations, output model, paging.

import (
	"strings"
	"testing"
)

const task4 = "Task 3 registers list_logs and task 4 completes it."

func TestListLogsTitleAndAnnotations(t *testing.T) {
	tool := logServer(t).tool(t, "list_logs", task4)
	checkTitle(t, tool, "List log files")
	checkReadAnnotations(t, tool)
}

func TestListLogsDescriptionLength(t *testing.T) {
	checkDescription(t, logServer(t).tool(t, "list_logs", task4))
}

func TestListLogsParameters(t *testing.T) {
	tool := logServer(t).tool(t, "list_logs", task4)
	checkPlain(t, tool, "path", "string", bound{"maxLength", 500}, bound{"default", "."})
	checkPlain(t, tool, "limit", "integer", bound{"minimum", 1}, bound{"maximum", 500}, bound{"default", 50})
	checkPlain(t, tool, "offset", "integer", bound{"minimum", 0}, bound{"default", 0})
	if req := requiredOf(tool); len(req) != 0 {
		t.Fatalf("list_logs: no parameter is required, got %v", req)
	}
}

func TestListLogsOutputSchema(t *testing.T) {
	tool := logServer(t).tool(t, "list_logs", task4)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "path", "files", "total", "shown", "note"); len(missing) > 0 {
		t.Fatalf("list_logs: the output schema must name path, files, total, shown and note, got %v", sortedKeys(fields))
	}
	items := outputFields(t, tool, "files")
	if missing := lacking(items, "path", "size_bytes", "compressed", "format"); len(missing) > 0 {
		t.Fatalf("list_logs: each file must name path, size_bytes, compressed and format, got %v", sortedKeys(items))
	}
}

func TestListLogsRefusesBadArguments(t *testing.T) {
	for _, c := range []badArgs{
		{"limit-0", map[string]any{"limit": 0}, "limit is at least 1"},
		{"limit-501", map[string]any{"limit": 501}, "limit is at most 500"},
		{"offset-negative", map[string]any{"offset": -1}, "offset is 0 or more"},
		{"path-too-long", map[string]any{"path": strings.Repeat("x", 501)}, "path is at most 500 characters"},
		{"unknown-argument", map[string]any{"surprise": 1}, "list_logs has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "list_logs", task4)
			refusedBySchema(t, s.call(t, "list_logs", c.args), c.why)
		})
	}
}

func TestListLogsAnswers(t *testing.T) {
	for _, id := range callsFor("list_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, servers(t)[calls[id].Root], id)
		})
	}
}

func TestListLogsRefusesAPathOutsideTheRoot(t *testing.T) {
	for _, path := range []string{"..", "../docs", "/etc"} {
		t.Run(path, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "list_logs", task4)
			toolError(t, s.call(t, "list_logs", map[string]any{"path": path}), "the path is outside the log root")
		})
	}
}
