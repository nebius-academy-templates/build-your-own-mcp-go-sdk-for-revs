package checks

// Task 3: group_lines: a group's lines from a scan, paging, the cap, E6 and E7.

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const task3 = "Task 3 registers group_lines."

var m3 = module("t3")

const goodID = "s_0123456789abcdef"

func topGroup() string {
	return calls["group-app-any-first"].GroupID
}

func TestGroupLinesTitleAndAnnotations(t *testing.T) {
	tool := m3.server(t).tool(t, "group_lines", task3)
	checkTitle(t, tool, "Show a group's lines")
	checkReadAnnotations(t, tool)
}

func TestGroupLinesDescriptionLength(t *testing.T) {
	checkDescription(t, m3.server(t).tool(t, "group_lines", task3))
}

func TestGroupLinesParameters(t *testing.T) {
	tool := m3.server(t).tool(t, "group_lines", task3)
	checkPlain(t, tool, "scan_id", "string", bound{"pattern", "^s_[0-9a-f]{16}$"})
	checkPlain(t, tool, "group_id", "string", bound{"pattern", "^[0-9a-f]{12}$"})
	checkPlain(t, tool, "limit", "integer", bound{"minimum", 1}, bound{"maximum", 200}, bound{"default", 50})
	checkPlain(t, tool, "offset", "integer", bound{"minimum", 0}, bound{"default", 0})
	if req := requiredOf(tool); strings.Join(req, ",") != "group_id,scan_id" {
		t.Fatalf("group_lines: scan_id and group_id are required, got %v", req)
	}
}

func TestGroupLinesOutputSchema(t *testing.T) {
	tool := m3.server(t).tool(t, "group_lines", task3)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "scan_id", "group_id", "pattern", "lines", "total", "shown", "note"); len(missing) > 0 {
		t.Fatalf("group_lines: the output schema lacks %v", missing)
	}
	line := outputFields(t, tool, "lines")
	if missing := lacking(line, "file", "line", "time", "text"); len(missing) > 0 {
		t.Fatalf("group_lines: a line lacks %v", missing)
	}
}

func TestGroupLinesRefusesBadArguments(t *testing.T) {
	top := topGroup()
	for _, c := range []badArgs{
		{"scan-id-shape", map[string]any{"scan_id": "scan-1", "group_id": top}, "scan_id matches ^s_[0-9a-f]{16}$"},
		{"scan-id-uppercase", map[string]any{"scan_id": "s_0123456789ABCDEF", "group_id": top}, "scan_id is lowercase hex"},
		{"group-id-shape", map[string]any{"scan_id": goodID, "group_id": "top"}, "group_id matches ^[0-9a-f]{12}$"},
		{"no-group-id", map[string]any{"scan_id": goodID}, "group_id is required"},
		{"no-scan-id", map[string]any{"group_id": top}, "scan_id is required"},
		{"limit-0", map[string]any{"scan_id": goodID, "group_id": top, "limit": 0}, "limit is at least 1"},
		{"limit-201", map[string]any{"scan_id": goodID, "group_id": top, "limit": 201}, "limit is at most 200"},
		{"offset-negative", map[string]any{"scan_id": goodID, "group_id": top, "offset": -1}, "offset is 0 or more"},
		{"unknown-argument", map[string]any{"scan_id": goodID, "group_id": top, "surprise": 1}, "group_lines has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m3.server(t)
			s.tool(t, "group_lines", task3)
			refusedBySchema(t, s.call(t, "group_lines", c.args), c.why)
		})
	}
}

func TestGroupLinesAnswers(t *testing.T) {
	for _, id := range callsFor("group_lines", "result") {
		t.Run(id, func(t *testing.T) {
			running := m3.servers(t)[calls[id].Root]
			running.tool(t, "group_lines", task3)
			checkAnswer(t, running, id, "")
		})
	}
}

func TestGroupLinesRefusesAGroupTheScanDoesNotKeep(t *testing.T) {
	for _, id := range callsFor("group_lines", "error") {
		t.Run(id, func(t *testing.T) {
			s := m3.server(t)
			s.tool(t, "group_lines", task3)
			checkError(t, s, id)
		})
	}
}

func TestUnknownScanID(t *testing.T) {
	s := m3.server(t)
	s.tool(t, "group_lines", task3)
	r := s.call(t, "group_lines", map[string]any{"scan_id": goodID, "group_id": topGroup()})
	toolError(t, r, e6(goodID), s.root)
}

func TestAScanIDExpires(t *testing.T) {
	s := m3.shortServer(t)
	s.tool(t, "group_lines", task3)
	scanID := s.scan(t, map[string]any{"path": appFile, "min_level": "any"})
	args := map[string]any{"scan_id": scanID, "group_id": topGroup(), "limit": 1}
	ok(t, s.call(t, "group_lines", args))
	time.Sleep(3 * time.Second)
	toolError(t, s.call(t, "group_lines", args), e6(scanID), s.root)
}

func TestThe21stScanDropsTheFirst(t *testing.T) {
	s := m3.server(t)
	s.tool(t, "group_lines", task3)
	var ids []string
	for range 21 {
		ids = append(ids, s.scan(t, map[string]any{"path": appFile, "min_level": "any"}))
	}
	first := map[string]any{"scan_id": ids[0], "group_id": topGroup(), "limit": 1}
	toolError(t, s.call(t, "group_lines", first), e6(ids[0]), s.root)
	for _, scanID := range []string{ids[1], ids[20]} {
		ok(t, s.call(t, "group_lines", map[string]any{"scan_id": scanID, "group_id": topGroup(), "limit": 1}))
	}
}

func TestScansMadeAtOnceAreAllKeptUpToTheCap(t *testing.T) {
	s := m3.server(t)
	s.tool(t, "group_lines", task3)
	arguments := make([]map[string]any, 25)
	for i := range arguments {
		arguments[i] = map[string]any{"path": appFile, "min_level": "any"}
	}
	var ids []string
	distinct := map[string]bool{}
	allShaped := true
	for _, r := range s.callMany(t, "summarize_logs", arguments) {
		scanID, _ := ok(t, r)["scan_id"].(string)
		ids = append(ids, scanID)
		distinct[scanID] = true
		allShaped = allShaped && scanIDRE.MatchString(scanID)
	}
	if len(distinct) != 25 || !allShaped {
		t.Fatal("25 scans made at the same time must give 25 different scan_ids")
	}
	kept := 0
	for _, scanID := range ids {
		if !s.call(t, "group_lines", map[string]any{"scan_id": scanID, "group_id": topGroup(), "limit": 1}).isError {
			kept++
		}
	}
	if kept != 20 {
		t.Fatalf("after 25 scans at once, exactly 20 must still answer (the cap per owner); %d did. "+
			"The scan store must be safe under concurrent calls.", kept)
	}
}

// Files added after the scan don't change its groups: group_lines doesn't scan again.
func TestGroupLinesReadsTheScanNotTheFolder(t *testing.T) {
	base := tempDir(t)
	entry := treeEntry{Path: "logs/worker.log", Numbered: true, Repeat: 30,
		Text: []string{"2026-01-15T11:00:00.000Z INFO worker job {n} done"}}
	if err := makeEntry(entry, base); err != nil {
		t.Fatal(err)
	}
	running := ownServer(t, filepath.Join(base, "logs"), filepath.Join(base, "reports"))
	running.tool(t, "group_lines", task3)
	scan := ok(t, running.call(t, "summarize_logs", map[string]any{"min_level": "any"}))
	groups, _ := scan["groups"].([]any)
	if len(groups) == 0 {
		t.Fatalf("summarize_logs found no group in %s", jsonText(scan))
	}
	group := groups[0].(map[string]any)["group_id"]
	entry.Path = "logs/worker-2.log"
	if err := makeEntry(entry, base); err != nil {
		t.Fatal(err)
	}
	result := ok(t, running.call(t, "group_lines", map[string]any{"scan_id": scan["scan_id"], "group_id": group, "limit": 200}))
	files := map[string]bool{}
	lines, _ := result["lines"].([]any)
	for _, line := range lines {
		file, _ := line.(map[string]any)["file"].(string)
		files[file] = true
	}
	if result["total"] != float64(30) || !slices.Equal(sortedKeys(files), []string{"worker.log"}) {
		t.Fatalf("group_lines must answer from the scan it names: a file added after the scan must not "+
			"appear, got total %s and files %v", jsonText(result["total"]), sortedKeys(files))
	}
}
