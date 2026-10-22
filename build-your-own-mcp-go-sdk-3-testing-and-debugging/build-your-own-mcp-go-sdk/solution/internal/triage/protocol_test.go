package triage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolListHasEveryTool(t *testing.T) {
	s := startServer(t, testRoot(t))
	res, err := s.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.OutputSchema == nil {
			t.Errorf("%s publishes no output schema", tool.Name)
		}
	}
	for _, want := range []string{"list_logs", "summarize_logs", "search_logs", "group_lines", "read_lines", "save_finding"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list lacks %s: %v", want, names)
		}
	}
}

func TestListLogsShowsEachFileAndFormat(t *testing.T) {
	s := startServer(t, testRoot(t))
	var out ListLogsOutput
	s.call(t, "list_logs", map[string]any{"path": "."}, &out)
	var got []string
	for _, f := range out.Files {
		got = append(got, f.Path+"="+f.Format)
	}
	want := "app/api.log=app notes/readme.txt=unknown web/access.log=nginx-access"
	if strings.Join(got, " ") != want || out.Total != 3 || out.Note != "" {
		t.Errorf("list_logs: %v (total %d, note %q), want %s", got, out.Total, out.Note, want)
	}
}

func TestSummarizeLogsCountsAndGroups(t *testing.T) {
	s := startServer(t, testRoot(t))
	var out SummarizeLogsOutput
	s.call(t, "summarize_logs", map[string]any{"min_level": "any"}, &out)
	if !regexp.MustCompile(`^s_[0-9a-f]{16}$`).MatchString(out.ScanID) {
		t.Errorf("scan_id %q", out.ScanID)
	}
	if out.FilesScanned != 2 || out.LinesScanned != 10 || out.LinesInWindow != 10 {
		t.Errorf("files %d, lines %d, in window %d; want 2, 10, 10", out.FilesScanned, out.LinesScanned, out.LinesInWindow)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != (SkippedFile{Path: "notes/readme.txt", Reason: "unknown format"}) {
		t.Errorf("skipped %+v", out.Skipped)
	}
	if want := (LevelCounts{Error: 4, Warning: 2, Info: 4}); out.ByLevel != want {
		t.Errorf("by_level %+v, want %+v", out.ByLevel, want)
	}
	// Two groups have three lines each; the one seen first comes first.
	first, second := out.Groups[0], out.Groups[1]
	if first.Pattern != "request served in <n> ms" || first.Count != 3 || first.Level != "info" || first.Example.Line != 1 {
		t.Errorf("first group %s", asJSON(first))
	}
	if second.Pattern != "upstream failed with code <n>" || second.Count != 3 || second.Level != "error" || second.Example.Line != 3 {
		t.Errorf("second group %s", asJSON(second))
	}
}

func TestSearchLogsPagesWhenTheTotalIsLargerThanThePage(t *testing.T) {
	s := startServer(t, testRoot(t))
	var first, second SearchLogsOutput
	s.call(t, "search_logs", map[string]any{"pattern": "served", "limit": 2}, &first)
	if first.Total != 3 || first.Shown != 2 || first.Note != "Showing 2 of 3 matches. Pass offset=2 for more." {
		t.Errorf("first page: total %d, shown %d, note %q", first.Total, first.Shown, first.Note)
	}
	s.call(t, "search_logs", map[string]any{"pattern": "served", "limit": 2, "offset": 2}, &second)
	if second.Total != 3 || second.Shown != 1 || second.Note != "" || second.Matches[0].Line != 6 {
		t.Errorf("second page: %s", asJSON(second))
	}
}

func TestGroupLinesPagesAGroupAndRefusesAnother(t *testing.T) {
	s := startServer(t, testRoot(t))
	var scan SummarizeLogsOutput
	s.call(t, "summarize_logs", map[string]any{"path": "app", "min_level": "error"}, &scan)
	group := scan.Groups[0].GroupID
	var out GroupLinesOutput
	s.call(t, "group_lines", map[string]any{"scan_id": scan.ScanID, "group_id": group, "limit": 2}, &out)
	if out.Total != 3 || out.Shown != 2 || out.Lines[0].Line != 3 || out.Lines[1].Line != 5 {
		t.Errorf("group_lines: %s", asJSON(out))
	}
	text := s.refusal(t, "group_lines", map[string]any{"scan_id": scan.ScanID, "group_id": "000000000000"})
	if want := `group "000000000000" is not in scan "` + scan.ScanID + `". Use a group_id from that scan's groups.`; text != want {
		t.Errorf("E7: %q", text)
	}
}

func TestReadLinesClipsAndRefuses(t *testing.T) {
	s := startServer(t, testRoot(t))
	var out ReadLinesOutput
	s.call(t, "read_lines", map[string]any{"path": "app/api.log", "line": 1, "before": 5, "after": 1}, &out)
	if out.Start != 1 || out.End != 2 || out.TotalLines != 7 || len(out.Lines) != 2 {
		t.Errorf("read_lines: %s", asJSON(out))
	}
	if text := s.refusal(t, "read_lines", map[string]any{"path": "app", "line": 1}); !strings.HasPrefix(text, `"app" is a folder.`) {
		t.Errorf("E10: %q", text)
	}
	if text := s.refusal(t, "read_lines", map[string]any{"path": "app/api.log", "line": 8}); text != `line 8 is past the end of "app/api.log", which has 7 lines.` {
		t.Errorf("E8: %q", text)
	}
	if text := s.refusal(t, "read_lines", map[string]any{"path": "../api.log", "line": 1}); !strings.HasPrefix(text, `path "../api.log" is outside the log root.`) {
		t.Errorf("E1: %q", text)
	}
}

func TestSaveFindingAndTheReportsResources(t *testing.T) {
	s := startServer(t, testRoot(t))
	if got := s.read(t, "triage://reports"); got != `{"count":0,"reports":[]}` {
		t.Errorf("empty reports: %s", got)
	}
	args := map[string]any{"title": "Upstream fails", "severity": "high", "summary": "Three failures.",
		"evidence": []any{map[string]any{"file": "app/api.log", "line": 3, "note": "the first one"}}}
	var saved SaveFindingOutput
	s.call(t, "save_finding", args, &saved)
	if saved.Name != "upstream-fails" {
		t.Errorf("name %q", saved.Name)
	}
	if text := s.refusal(t, "save_finding", args); text != `a finding named "upstream-fails" already exists. Choose a different title.` {
		t.Errorf("E9: %q", text)
	}
	s.reconnect(t)
	var listed struct {
		Count   int          `json:"count"`
		Reports []reportInfo `json:"reports"`
	}
	if err := json.Unmarshal([]byte(s.read(t, "triage://reports")), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Count != 1 || listed.Reports[0] != (reportInfo{Name: "upstream-fails", Title: "Upstream fails", Severity: "high", Created: saved.Created}) {
		t.Errorf("reports after a save: %+v", listed)
	}
	file, err := os.ReadFile(filepath.Join(s.reports, "upstream-fails.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.read(t, "triage://reports/upstream-fails"); got != string(file) {
		t.Errorf("the report resource differs from the file")
	}
}

func TestFormatsAndScanResources(t *testing.T) {
	s := startServer(t, testRoot(t))
	var doc struct {
		Formats []formatInfo `json:"formats"`
	}
	if err := json.Unmarshal([]byte(s.read(t, "triage://formats")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Formats) != 9 || doc.Formats[0].Name != "app" || doc.Formats[8].Name != "gotest" {
		t.Errorf("formats: %d, first %q", len(doc.Formats), doc.Formats[0].Name)
	}
	var scan SummarizeLogsOutput
	s.call(t, "summarize_logs", map[string]any{"path": "web"}, &scan)
	var kept SummarizeLogsOutput
	if err := json.Unmarshal([]byte(s.read(t, "triage://scans/"+scan.ScanID)), &kept); err != nil {
		t.Fatal(err)
	}
	if kept.ScanID != scan.ScanID || kept.LinesScanned != 3 || kept.TotalGroups != scan.TotalGroups {
		t.Errorf("scan resource %s", asJSON(kept))
	}
	_, err := s.session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "triage://scans/s_0000000000000000"})
	if err == nil || !strings.Contains(err.Error(), "Resource not found") {
		t.Errorf("an unknown scan: %v", err)
	}
}

// scanOf runs summarize_logs with args and returns its scan_id.
func (s *testServer) scanOf(t *testing.T, args map[string]any) string {
	t.Helper()
	var out SummarizeLogsOutput
	s.call(t, "summarize_logs", args, &out)
	return out.ScanID
}

// bucketCounts lists each bucket as "<start> <count>".
func bucketCounts(out TimelineOutput) string {
	var parts []string
	for _, b := range out.Buckets {
		parts = append(parts, b.Start+" "+strconv.Itoa(b.Count))
	}
	return strings.Join(parts, ", ")
}

func TestTimelineCountsEachBucketAndKeepsEmptyOnes(t *testing.T) {
	s := startServer(t, testRoot(t))
	scan := s.scanOf(t, map[string]any{"path": "app", "since": "2026-01-15T10:00", "until": "2026-01-15T12:00"})
	var all, failed TimelineOutput
	s.call(t, "timeline", map[string]any{"scan_id": scan, "bucket_minutes": 30}, &all)
	want := "2026-01-15T10:00:00Z 6, 2026-01-15T10:30:00Z 0, 2026-01-15T11:00:00Z 1, 2026-01-15T11:30:00Z 0"
	if got := bucketCounts(all); got != want || all.Total != 7 {
		t.Errorf("every line: %s (total %d), want %s (total 7)", got, all.Total, want)
	}
	if deref(all.Start) != "2026-01-15T10:00:00Z" || deref(all.End) != "2026-01-15T12:00:00Z" || all.Pattern != nil {
		t.Errorf("start %s, end %s, pattern %v", deref(all.Start), deref(all.End), all.Pattern)
	}
	s.call(t, "timeline", map[string]any{"scan_id": scan, "bucket_minutes": 30, "pattern": "upstream failed"}, &failed)
	want = "2026-01-15T10:00:00Z 2, 2026-01-15T10:30:00Z 0, 2026-01-15T11:00:00Z 1, 2026-01-15T11:30:00Z 0"
	if got := bucketCounts(failed); got != want || failed.Total != 3 || deref(failed.Pattern) != "upstream failed" {
		t.Errorf("upstream failed: %s (total %d), want %s (total 3)", got, failed.Total, want)
	}
}

func TestTimelineAnchorsItsGridAtMidnight(t *testing.T) {
	s := startServer(t, testRoot(t))
	scan := s.scanOf(t, map[string]any{"path": "app"})
	var out TimelineOutput
	s.call(t, "timeline", map[string]any{"scan_id": scan, "bucket_minutes": 7, "pattern": "WARN|ERROR"}, &out)
	// The first counted line is at 10:00:05, 600 minutes after midnight: 85 buckets of 7 and 5 minutes over.
	// The last, at 11:00, is 65 minutes after 09:55, in the tenth bucket.
	if deref(out.Start) != "2026-01-15T09:55:00Z" || deref(out.End) != "2026-01-15T11:05:00Z" {
		t.Errorf("start %s, end %s; want 09:55 and 11:05", deref(out.Start), deref(out.End))
	}
	if len(out.Buckets) != 10 || out.Total != 4 || out.Buckets[0].Count != 2 || out.Buckets[1].Count != 1 || out.Buckets[9].Count != 1 {
		t.Errorf("buckets: %s", asJSON(out))
	}
}

func TestTimelineValuesPerBucket(t *testing.T) {
	s := startServer(t, testRoot(t))
	scan := s.scanOf(t, map[string]any{"path": "app"})
	var out TimelineOutput
	s.call(t, "timeline", map[string]any{"scan_id": scan, "value_pattern": `in (\d+) ms`}, &out)
	if len(out.Buckets) != 2 {
		t.Fatalf("buckets: %s", asJSON(out))
	}
	first, second := out.Buckets[0], out.Buckets[1]
	// 12, 812, 15 and 9 ms: the median is the mean of 12 and 15.
	if first.Count != 6 || first.ValueCount != 4 || *first.Min != 9 || *first.Median != 13.5 || *first.Max != 812 {
		t.Errorf("10:00 bucket: %s", asJSON(first))
	}
	if second.Count != 1 || second.ValueCount != 0 || second.Min != nil || second.Median != nil || second.Max != nil {
		t.Errorf("11:00 bucket: %s", asJSON(second))
	}
}

func TestTimelineMedianRoundsTiesToEvenAndSkipsOtherNumbers(t *testing.T) {
	root := t.TempDir()
	lines := []string{
		"2026-01-15T10:00:00.000Z INFO api value=0",
		"2026-01-15T10:00:01.000Z INFO api value=0.125",
		"2026-01-15T10:00:02.000Z INFO api value=1e3",
		"2026-01-15T10:00:03.000Z INFO api value=.5",
		"2026-01-15T10:00:04.000Z INFO api value=none",
	}
	if err := os.WriteFile(filepath.Join(root, "values.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startServer(t, root)
	scan := s.scanOf(t, map[string]any{})
	var out TimelineOutput
	s.call(t, "timeline", map[string]any{"scan_id": scan, "value_pattern": `value=(\S+)`}, &out)
	b := out.Buckets[0]
	if b.Count != 5 || b.ValueCount != 2 || *b.Min != 0 || *b.Median != 0.062 || *b.Max != 0.125 {
		t.Errorf("bucket: %s; want 5 lines, 2 values, median 0.062", asJSON(b))
	}
}

func TestTimelineWithNoCountedLineHasNoBuckets(t *testing.T) {
	s := startServer(t, testRoot(t))
	scan := s.scanOf(t, map[string]any{"path": "app"})
	var out TimelineOutput
	s.call(t, "timeline", map[string]any{"scan_id": scan, "pattern": "no such line"}, &out)
	if out.Start != nil || out.End != nil || out.Total != 0 || out.Buckets == nil || len(out.Buckets) != 0 {
		t.Errorf("empty timeline: %s", asJSON(out))
	}
}

func TestTimelineRefusals(t *testing.T) {
	s := startServer(t, testRoot(t))
	scan := s.scanOf(t, map[string]any{"path": "app", "since": "2026-01-15", "until": "2026-01-17"})
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown scan", map[string]any{"scan_id": "s_0000000000000000", "pattern": "("},
			`scan "s_0000000000000000" is unknown or has expired. Call summarize_logs again for a new scan_id.`},
		{"bad pattern first", map[string]any{"scan_id": scan, "pattern": "(", "value_pattern": "("},
			"pattern is not a valid regular expression. Escape special characters such as ( [ . * with a backslash, or search for a plain word."},
		{"bad value pattern", map[string]any{"scan_id": scan, "value_pattern": "("},
			"value_pattern is not a valid regular expression. Escape special characters such as ( [ . * with a backslash, or search for a plain word."},
		{"no capture group", map[string]any{"scan_id": scan, "value_pattern": `(?:\d+)`},
			`value_pattern must contain exactly one capture group, for example "size=(\d+)".`},
		{"two capture groups", map[string]any{"scan_id": scan, "value_pattern": `(\d+)-(\d+)`, "bucket_minutes": 1},
			`value_pattern must contain exactly one capture group, for example "size=(\d+)".`},
		{"too many buckets", map[string]any{"scan_id": scan, "bucket_minutes": 1},
			"the window needs 2880 buckets and the limit is 500. Pass a larger bucket_minutes."},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := s.refusal(t, "timeline", c.args); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}
