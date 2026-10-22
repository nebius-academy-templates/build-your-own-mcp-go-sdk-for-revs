package checks

// Task 6: search_logs: matching lines, their order, window, level and paging.

import (
	"strings"
	"testing"
)

const task6 = "Task 6 registers search_logs."

func TestSearchLogsTitleAndAnnotations(t *testing.T) {
	tool := logServer(t).tool(t, "search_logs", task6)
	checkTitle(t, tool, "Search logs")
	checkReadAnnotations(t, tool)
}

func TestSearchLogsDescriptionLength(t *testing.T) {
	checkDescription(t, logServer(t).tool(t, "search_logs", task6))
}

func TestSearchLogsParameters(t *testing.T) {
	tool := logServer(t).tool(t, "search_logs", task6)
	checkPlain(t, tool, "path", "string", bound{"maxLength", 500}, bound{"default", "."})
	checkPlain(t, tool, "pattern", "string", bound{"minLength", 1}, bound{"maxLength", 200})
	checkNullableString(t, tool, "since", bound{"maxLength", 40})
	checkNullableString(t, tool, "until", bound{"maxLength", 40})
	checkEnum(t, tool, "min_level", levels, "any")
	checkPlain(t, tool, "limit", "integer", bound{"minimum", 1}, bound{"maximum", 200}, bound{"default", 50})
	checkPlain(t, tool, "offset", "integer", bound{"minimum", 0}, bound{"default", 0})
	if req := requiredOf(tool); strings.Join(req, ",") != "pattern" {
		t.Fatalf("search_logs: only pattern is required, got %v", req)
	}
}

func TestSearchLogsOutputSchema(t *testing.T) {
	tool := logServer(t).tool(t, "search_logs", task6)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "path", "pattern", "matches", "total", "shown", "note"); len(missing) > 0 {
		t.Fatalf("search_logs: the output schema lacks %v", missing)
	}
	match := outputFields(t, tool, "matches")
	if missing := lacking(match, "file", "line", "time", "level", "text"); len(missing) > 0 {
		t.Fatalf("search_logs: a match lacks %v", missing)
	}
}

func TestSearchLogsRefusesBadArguments(t *testing.T) {
	for _, c := range []badArgs{
		{"no-pattern", map[string]any{}, "pattern is required"},
		{"empty-pattern", map[string]any{"pattern": ""}, "pattern is at least 1 character"},
		{"pattern-too-long", map[string]any{"pattern": strings.Repeat("x", 201)}, "pattern is at most 200 characters"},
		{"min-level-fatal", map[string]any{"pattern": "x", "min_level": "fatal"}, "min_level is one of " + strings.Join(levels, ", ")},
		{"limit-201", map[string]any{"pattern": "x", "limit": 201}, "limit is at most 200"},
		{"offset-negative", map[string]any{"pattern": "x", "offset": -1}, "offset is 0 or more"},
		{"unknown-argument", map[string]any{"pattern": "x", "surprise": 1}, "search_logs has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "search_logs", task6)
			refusedBySchema(t, s.call(t, "search_logs", c.args), c.why)
		})
	}
}

func TestSearchLogsAnswers(t *testing.T) {
	for _, id := range callsFor("search_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, servers(t)[calls[id].Root], id)
		})
	}
}

func TestSearchLogsRefusesAPathOutsideTheRoot(t *testing.T) {
	for _, path := range []string{"..", "/etc"} {
		t.Run(path, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "search_logs", task6)
			toolError(t, s.call(t, "search_logs", map[string]any{"path": path, "pattern": "x"}), "the path is outside the log root")
		})
	}
}
