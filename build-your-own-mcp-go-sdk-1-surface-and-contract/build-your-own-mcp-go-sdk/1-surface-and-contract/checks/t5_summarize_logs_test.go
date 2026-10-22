package checks

// Task 5: summarize_logs: window, level counts, message groups, limit and note.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const task5 = "Task 5 registers summarize_logs."

func TestSummarizeLogsTitleAndAnnotations(t *testing.T) {
	tool := logServer(t).tool(t, "summarize_logs", task5)
	checkTitle(t, tool, "Summarize logs")
	checkReadAnnotations(t, tool)
}

func TestSummarizeLogsDescriptionLength(t *testing.T) {
	checkDescription(t, logServer(t).tool(t, "summarize_logs", task5))
}

func TestSummarizeLogsParameters(t *testing.T) {
	tool := logServer(t).tool(t, "summarize_logs", task5)
	checkPlain(t, tool, "path", "string", bound{"maxLength", 500}, bound{"default", "."})
	checkNullableString(t, tool, "since", bound{"maxLength", 40})
	checkNullableString(t, tool, "until", bound{"maxLength", 40})
	checkEnum(t, tool, "min_level", levels, "warning")
	checkPlain(t, tool, "limit", "integer", bound{"minimum", 1}, bound{"maximum", 100}, bound{"default", 20})
	if req := requiredOf(tool); len(req) != 0 {
		t.Fatalf("summarize_logs: no parameter is required, got %v", req)
	}
}

func TestSummarizeLogsOutputSchema(t *testing.T) {
	tool := logServer(t).tool(t, "summarize_logs", task5)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "path", "since", "until", "min_level", "files_scanned", "skipped",
		"lines_scanned", "lines_in_window", "first_seen", "last_seen", "by_level", "groups", "total_groups",
		"shown", "note"); len(missing) > 0 {
		t.Fatalf("summarize_logs: the output schema lacks %v", missing)
	}
	group := outputFields(t, tool, "groups")
	if missing := lacking(group, "group_id", "format", "level", "pattern", "count", "first_seen", "last_seen",
		"file_count", "files", "example"); len(missing) > 0 {
		t.Fatalf("summarize_logs: a group lacks %v", missing)
	}
	example := outputFields(t, tool, "groups", "example")
	if missing := lacking(example, "file", "line", "text"); len(missing) > 0 {
		t.Fatalf("summarize_logs: example lacks %v", missing)
	}
}

func TestSummarizeLogsRefusesBadArguments(t *testing.T) {
	for _, c := range []badArgs{
		{"min-level-fatal", map[string]any{"min_level": "fatal"}, "min_level is one of " + strings.Join(levels, ", ")},
		{"limit-0", map[string]any{"limit": 0}, "limit is at least 1"},
		{"limit-101", map[string]any{"limit": 101}, "limit is at most 100"},
		{"since-too-long", map[string]any{"since": "2026-01-01T00:00:00.000000000000000000000000000Z"}, "since is at most 40 characters"},
		{"unknown-argument", map[string]any{"surprise": 1}, "summarize_logs has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "summarize_logs", task5)
			refusedBySchema(t, s.call(t, "summarize_logs", c.args), c.why)
		})
	}
}

func TestSummarizeLogsAnswers(t *testing.T) {
	for _, id := range callsFor("summarize_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, servers(t)[calls[id].Root], id)
		})
	}
}

func groupLevels(result map[string]any) map[any]bool {
	found := map[any]bool{}
	groups, _ := result["groups"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		found[group["level"]] = true
	}
	return found
}

func TestMinLevelAnyIncludesUnknown(t *testing.T) {
	s := logServer(t)
	s.tool(t, "summarize_logs", task5)
	if !groupLevels(checkAnswer(t, s, "summarize-redis-any"))["unknown"] {
		t.Fatal("min_level any must include groups of level unknown")
	}
	if groupLevels(checkAnswer(t, s, "summarize-redis-debug"))["unknown"] {
		t.Fatal("min_level debug must leave out groups of level unknown")
	}
}

func TestNoLogLineOutsideExample(t *testing.T) {
	s := logServer(t)
	s.tool(t, "summarize_logs", task5)
	c := calls["summarize-app-any"]
	result := checkAnswer(t, s, "summarize-app-any")
	path, _ := c.Args["path"].(string)
	lines := map[string]bool{}
	for _, line := range rawLines(t, filepath.Join(s3Logs, filepath.FromSlash(path))) {
		lines[line] = true
	}
	var walk func(value any, where string)
	walk = func(value any, where string) {
		switch v := value.(type) {
		case map[string]any:
			for _, key := range sortedKeys(v) {
				if key != "example" {
					walk(v[key], where+"."+key)
				}
			}
		case []any:
			for i, item := range v {
				walk(item, fmt.Sprintf("%s[%d]", where, i))
			}
		case string:
			if lines[v] {
				t.Fatalf("summarize_logs%s holds a raw log line; only example.text may", where)
			}
		}
	}
	walk(result, "")
}

func TestSummarizeLogsRefusesAPathOutsideTheRoot(t *testing.T) {
	for _, path := range []string{"..", "/etc"} {
		t.Run(path, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "summarize_logs", task5)
			toolError(t, s.call(t, "summarize_logs", map[string]any{"path": path}), "the path is outside the log root")
		})
	}
}
