package triage

import (
	"strings"
	"testing"
)

// A scan, then one of its groups, then the lines around one of that group's lines.
func TestInvestigationFromScanToLines(t *testing.T) {
	s := startServer(t, testRoot(t))
	var scan SummarizeLogsOutput
	s.call(t, "summarize_logs", map[string]any{"min_level": "warning"}, &scan)
	if len(scan.Groups) == 0 {
		t.Fatal("the scan found no group")
	}
	top := scan.Groups[0]
	var lines GroupLinesOutput
	s.call(t, "group_lines", map[string]any{"scan_id": scan.ScanID, "group_id": top.GroupID}, &lines)
	if lines.Total != top.Count || lines.Shown != top.Count || lines.Pattern != top.Pattern {
		t.Fatalf("group_lines for %s: total %d, shown %d, pattern %q; the scan said %d and %q",
			top.GroupID, lines.Total, lines.Shown, lines.Pattern, top.Count, top.Pattern)
	}
	last := lines.Lines[len(lines.Lines)-1]
	var around ReadLinesOutput
	s.call(t, "read_lines", map[string]any{"path": last.File, "line": last.Line, "before": 1, "after": 1}, &around)
	if around.File != last.File || around.Start != last.Line-1 {
		t.Fatalf("read_lines around %s:%d: %s", last.File, last.Line, asJSON(around))
	}
	found := false
	for _, l := range around.Lines {
		if l.Line == last.Line {
			found = l.Text == last.Text && strings.Contains(l.Text, "upstream failed")
		}
	}
	if !found {
		t.Errorf("read_lines doesn't show the group's line %d as group_lines did: %s", last.Line, asJSON(around.Lines))
	}
}

// Two scans of the same path give the same counts and groups under different ids.
func TestTheSameScanTwiceGivesTheSameCounts(t *testing.T) {
	s := startServer(t, testRoot(t))
	var first, second SummarizeLogsOutput
	args := map[string]any{"min_level": "any", "since": "2026-01-15T10:00", "until": "2026-01-15T10:30"}
	s.call(t, "summarize_logs", args, &first)
	s.call(t, "summarize_logs", args, &second)
	if first.ScanID == second.ScanID {
		t.Error("two scans share a scan_id")
	}
	if first.ByLevel != second.ByLevel || first.LinesInWindow != second.LinesInWindow || first.TotalGroups != second.TotalGroups {
		t.Errorf("the second scan differs: %+v then %+v", first.ByLevel, second.ByLevel)
	}
	if want := (LevelCounts{Error: 3, Warning: 2, Info: 4}); first.ByLevel != want || first.LinesInWindow != 9 {
		t.Errorf("the window holds %d lines, %+v; want 9 lines, %+v", first.LinesInWindow, first.ByLevel, want)
	}
}
