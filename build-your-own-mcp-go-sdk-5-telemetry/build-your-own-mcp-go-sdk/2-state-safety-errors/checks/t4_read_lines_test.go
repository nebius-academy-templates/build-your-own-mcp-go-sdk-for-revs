package checks

// Task 4: read_lines: the lines around one line, clipped to the file, E8 and E10.

import (
	"strings"
	"testing"
)

const task4 = "Task 4 registers read_lines."

var m4 = module("t4")

func TestReadLinesTitleAndAnnotations(t *testing.T) {
	tool := m4.server(t).tool(t, "read_lines", task4)
	checkTitle(t, tool, "Read lines around a line")
	checkReadAnnotations(t, tool)
}

func TestReadLinesDescriptionLength(t *testing.T) {
	checkDescription(t, m4.server(t).tool(t, "read_lines", task4))
}

func TestReadLinesParameters(t *testing.T) {
	tool := m4.server(t).tool(t, "read_lines", task4)
	checkPlain(t, tool, "path", "string", bound{"maxLength", 500})
	checkPlain(t, tool, "line", "integer", bound{"minimum", 1})
	checkPlain(t, tool, "before", "integer", bound{"minimum", 0}, bound{"maximum", 50}, bound{"default", 5})
	checkPlain(t, tool, "after", "integer", bound{"minimum", 0}, bound{"maximum", 200}, bound{"default", 20})
	if req := requiredOf(tool); strings.Join(req, ",") != "line,path" {
		t.Fatalf("read_lines: path and line are required, got %v", req)
	}
}

func TestReadLinesOutputSchema(t *testing.T) {
	tool := m4.server(t).tool(t, "read_lines", task4)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "file", "start", "end", "total_lines", "lines"); len(missing) > 0 {
		t.Fatalf("read_lines: the output schema lacks %v", missing)
	}
	line := outputFields(t, tool, "lines")
	if missing := lacking(line, "line", "text"); len(missing) > 0 {
		t.Fatalf("read_lines: a line lacks %v", missing)
	}
}

func TestReadLinesRefusesBadArguments(t *testing.T) {
	for _, c := range []badArgs{
		{"no-line", map[string]any{"path": appFile}, "line is required"},
		{"no-path", map[string]any{"line": 1}, "path is required"},
		{"line-0", map[string]any{"path": appFile, "line": 0}, "line is 1 or more"},
		{"before-51", map[string]any{"path": appFile, "line": 1, "before": 51}, "before is at most 50"},
		{"before-negative", map[string]any{"path": appFile, "line": 1, "before": -1}, "before is 0 or more"},
		{"after-201", map[string]any{"path": appFile, "line": 1, "after": 201}, "after is at most 200"},
		{"path-too-long", map[string]any{"path": strings.Repeat("x", 501), "line": 1}, "path is at most 500 characters"},
		{"unknown-argument", map[string]any{"path": appFile, "line": 1, "surprise": 1}, "read_lines has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m4.server(t)
			s.tool(t, "read_lines", task4)
			refusedBySchema(t, s.call(t, "read_lines", c.args), c.why)
		})
	}
}

func TestReadLinesAnswers(t *testing.T) {
	for _, id := range callsFor("read_lines", "result") {
		t.Run(id, func(t *testing.T) {
			running := m4.servers(t)[calls[id].Root]
			running.tool(t, "read_lines", task4)
			checkAnswer(t, running, id, "")
		})
	}
}

func TestReadLinesRefusesPastTheEndAndFolders(t *testing.T) {
	for _, id := range callsFor("read_lines", "error") {
		t.Run(id, func(t *testing.T) {
			s := m4.server(t)
			s.tool(t, "read_lines", task4)
			checkError(t, s, id)
		})
	}
}
