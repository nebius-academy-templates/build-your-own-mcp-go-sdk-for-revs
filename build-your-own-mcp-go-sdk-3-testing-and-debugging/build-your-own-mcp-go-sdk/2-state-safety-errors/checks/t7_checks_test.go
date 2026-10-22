package checks

// Task 7: checks behind the schema (E3, E4, E5, E9, evidence), closed evidence items, masking.

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var m7 = module("t7")

var goodItem = map[string]any{"file": phpFile, "line": 2, "note": "the pool starts"}

func phpLines() int {
	result, _ := calls["read-php-dot-slash"].Result.(map[string]any)
	total, _ := result["total_lines"].(float64)
	return int(total)
}

func checkedFinding(title string, evidence ...map[string]any) map[string]any {
	items := []any{}
	for _, item := range evidence {
		items = append(items, item)
	}
	if len(items) == 0 {
		items = append(items, goodItem)
	}
	return map[string]any{"title": title, "severity": "medium", "summary": "Checked twice.", "evidence": items}
}

func TestSaveFindingMetadataMatchesCreateOnlyBehavior(t *testing.T) {
	tool := m7.server(t).tool(t, "save_finding", "")
	hints, _ := tool["annotations"].(object)
	if destructive, has := hints["destructiveHint"]; !has || destructive != false {
		t.Fatal("save_finding: destructiveHint must be false because an existing report is left unchanged")
	}
}

// written lists the files in the server's reports folder.
func written(t *testing.T, s *server) []string {
	t.Helper()
	entries, err := os.ReadDir(s.reports)
	if os.IsNotExist(err) {
		return []string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return slices.Sorted(slices.Values(names))
}

func TestAWindowEndThatDoesNotRead(t *testing.T) {
	for _, c := range []struct {
		id, tool string
		args     map[string]any
		field    string
	}{
		{"summarize-since-word", "summarize_logs", map[string]any{"path": appFile, "since": "yesterday"}, "since"},
		{"summarize-until-month-13", "summarize_logs", map[string]any{"path": appFile, "until": "2026-13-01"}, "until"},
		{"search-since-word", "search_logs", map[string]any{"path": appFile, "pattern": "quota", "since": "noon"}, "since"},
		{"search-until-space", "search_logs", map[string]any{"path": appFile, "pattern": "quota", "until": "2026-08-30 10:00"}, "until"},
		{"search-until-hour-24", "search_logs", map[string]any{"path": appFile, "pattern": "quota", "until": "2026-08-30T24:00"}, "until"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m7.server(t)
			s.tool(t, c.tool, "")
			toolError(t, s.call(t, c.tool, c.args), e3(c.field), s.root)
		})
	}
}

func TestUntilBeforeSince(t *testing.T) {
	for _, c := range []struct {
		id, tool string
		args     map[string]any
	}{
		{"summarize-equal", "summarize_logs", map[string]any{"path": appFile, "since": "2026-08-30T10:00", "until": "2026-08-30T10:00:00Z"}},
		{"summarize-reversed", "summarize_logs", map[string]any{"path": appFile, "since": "2026-08-31", "until": "2026-08-30"}},
		{"search-equal-after-zones", "search_logs", map[string]any{"path": appFile, "pattern": "quota",
			"since": "2026-08-30T12:00+02:00", "until": "2026-08-30T10:00Z"}},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m7.server(t)
			s.tool(t, c.tool, "")
			toolError(t, s.call(t, c.tool, c.args), e4, s.root)
		})
	}
}

func TestAPatternThatDoesNotCompile(t *testing.T) {
	for _, pattern := range []string{"quota(", "[unclosed", "*quota"} {
		t.Run(pattern, func(t *testing.T) {
			s := m7.server(t)
			s.tool(t, "search_logs", "")
			toolError(t, s.call(t, "search_logs", map[string]any{"path": appFile, "pattern": pattern}), e5("pattern"), s.root)
		})
	}
}

func TestAnExistingFindingIsNotOverwritten(t *testing.T) {
	s := m7.server(t)
	s.tool(t, "save_finding", "")
	ok(t, s.call(t, "save_finding", checkedFinding("Nightly sync ran late")))
	file := filepath.Join(s.reports, "nightly-sync-ran-late.md")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	args := checkedFinding("Nightly sync ran late!")
	args["severity"] = "critical"
	toolError(t, s.call(t, "save_finding", args), e9("nightly-sync-ran-late"), s.root)
	if after, err := os.ReadFile(file); err != nil || !bytes.Equal(after, before) {
		t.Fatal("a refused save_finding must leave the existing file as it was")
	}
}

func TestEvidenceIsCheckedAndNothingIsWritten(t *testing.T) {
	lines := phpLines()
	for _, c := range []struct {
		id   string
		item map[string]any
		text string
	}{
		{"outside", map[string]any{"file": "../outside.log", "line": 1, "note": "x"}, e1("../outside.log")},
		{"absolute-outside", map[string]any{"file": "/etc/hosts", "line": 1, "note": "x"}, e1("/etc/hosts")},
		{"missing", map[string]any{"file": "vms/web-01/archive/no-such-file.log.gz", "line": 1, "note": "x"},
			e2("vms/web-01/archive/no-such-file.log.gz")},
		{"folder", map[string]any{"file": "vms/web-01", "line": 1, "note": "x"}, e10("vms/web-01")},
		{"root", map[string]any{"file": ".", "line": 1, "note": "x"}, e10(".")},
		{"past-the-end", map[string]any{"file": phpFile, "line": lines + 1, "note": "x"}, e8(lines+1, phpFile, lines)},
		{"past-the-end-dot-slash", map[string]any{"file": "./" + phpFile, "line": 99, "note": "x"}, e8(99, phpFile, lines)},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m7.server(t)
			s.tool(t, "save_finding", "")
			before := written(t, s)
			r := s.call(t, "save_finding", checkedFinding("Evidence under review", goodItem, c.item))
			toolError(t, r, c.text, s.root)
			if after := written(t, s); !slices.Equal(after, before) {
				t.Fatal("a refused save_finding must write nothing")
			}
		})
	}
}

func TestAnExistingNameIsRefusedBeforeTheEvidence(t *testing.T) {
	s := m7.server(t)
	s.tool(t, "save_finding", "")
	ok(t, s.call(t, "save_finding", checkedFinding("Backup window overlaps")))
	bad := map[string]any{"file": "vms/web-01", "line": 1, "note": "x"}
	r := s.call(t, "save_finding", checkedFinding("Backup window overlaps", bad))
	toolError(t, r, e9("backup-window-overlaps"), s.root)
}

func TestEvidenceItemsAreCheckedInOrder(t *testing.T) {
	s := m7.server(t)
	s.tool(t, "save_finding", "")
	first := map[string]any{"file": "nope.log", "line": 1, "note": "x"}
	second := map[string]any{"file": "vms/web-01", "line": 1, "note": "x"}
	r := s.call(t, "save_finding", checkedFinding("Two bad items", goodItem, first, second))
	toolError(t, r, e2("nope.log"), s.root)
}

func TestEvidenceItemsAreClosed(t *testing.T) {
	s := m7.server(t)
	tool := s.tool(t, "save_finding", "")
	root, _ := tool["inputSchema"].(object)
	props, _ := root["properties"].(object)
	var items []any
	for _, branch := range branches(t, props["evidence"], root) {
		if item, has := branch["items"]; has && item != nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		t.Fatal("save_finding.evidence: the schema must describe its items")
	}
	item := resolve(t, items[0], root)
	if item["additionalProperties"] != false {
		t.Fatalf("save_finding.evidence items must refuse keys other than file, line and note "+
			"(additionalProperties: false), got %s", jsonText(item["additionalProperties"]))
	}
	before := written(t, s)
	extra := map[string]any{"severity": "high"}
	for key, value := range goodItem {
		extra[key] = value
	}
	r := s.call(t, "save_finding", checkedFinding("Closed items", extra))
	refusedBySchema(t, r, "an evidence item has only file, line and note")
	if after := written(t, s); !slices.Equal(after, before) {
		t.Fatal("a refused save_finding must write nothing")
	}
}

func TestAGoodFindingIsStillSaved(t *testing.T) {
	s := m7.server(t)
	s.tool(t, "save_finding", "")
	item := map[string]any{"file": "./" + phpFile, "line": phpLines(), "note": "the last line"}
	result := ok(t, s.call(t, "save_finding", checkedFinding("Pool stops cleanly", item)))
	if result["name"] != "pool-stops-cleanly" {
		t.Fatalf("save_finding: name %s", jsonText(result["name"]))
	}
	if info, err := os.Stat(filepath.Join(s.reports, "pool-stops-cleanly.md")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("a finding whose evidence checks out is written")
	}
}

func TestUnexpectedErrorsAreMasked(t *testing.T) {
	hostile := treeFor(t, "hostile")
	s := m7.hostileServer(t)
	s.tool(t, "read_lines", "Task 4 registers read_lines.")
	r := s.call(t, "read_lines", map[string]any{"path": "broken/bad.log.gz", "line": 1})
	masked(t, r, hostile.root)
}
