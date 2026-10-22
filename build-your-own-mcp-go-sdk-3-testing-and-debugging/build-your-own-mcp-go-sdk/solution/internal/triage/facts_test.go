package triage

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
)

// The repository's log bundle and the practice's facts, from this package's folder.
var (
	bundleDir = filepath.Join("..", "..", "..", "s3_logs")
	factsPath = filepath.Join("..", "..", "..", "3-testing-and-debugging", "facts.json")
)

// fileFact is one file of facts.json.
type fileFact struct {
	Path               string      `json:"path"`
	Format             string      `json:"format"`
	Lines              int         `json:"lines"`
	LinesWithOwnTime   int         `json:"lines_with_own_time"`
	UntimedLines       int         `json:"untimed_lines"`
	FirstSeen          *string     `json:"first_seen"`
	LastSeen           *string     `json:"last_seen"`
	ByLevel            LevelCounts `json:"by_level"`
	RunHeaders         *int        `json:"run_headers"`
	DistinctRunHeaders *int        `json:"distinct_run_headers"`
	LinesBeforeDate    int         `json:"lines_before_name_date"`
	Window             *struct {
		Since         string `json:"since"`
		Until         string `json:"until"`
		LinesAtSince  int    `json:"lines_at_since"`
		LinesAtUntil  int    `json:"lines_at_until"`
		LinesInWindow int    `json:"lines_in_window"`
	} `json:"window"`
	TopGroups []struct {
		GroupID string `json:"group_id"`
		Count   int    `json:"count"`
	} `json:"top_groups"`
}

type factsFile struct {
	Files     []fileFact `json:"files"`
	GzVsPlain []string   `json:"gz_vs_plain"`
}

func loadFacts(t *testing.T) factsFile {
	t.Helper()
	data, err := os.ReadFile(factsPath)
	if err != nil {
		t.Fatalf("reading facts.json: %v", err)
	}
	var facts factsFile
	if err := json.Unmarshal(data, &facts); err != nil {
		t.Fatalf("facts.json: %v", err)
	}
	if len(facts.Files) == 0 {
		t.Fatal("facts.json lists no files")
	}
	return facts
}

// readFactFile reads one file of the bundle with the server's reader.
func readFactFile(t *testing.T, rel string) []string {
	t.Helper()
	dir, err := paths.Resolve(bundleDir)
	if err != nil {
		t.Fatal(err)
	}
	root := logs.Root{Root: dir}
	target, err := root.Resolve(rel)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := root.Files(target)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := logs.ReadLines(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	return texts
}

// ownTimes gives each line its own time, or nil when the line borrows one.
func ownTimes(texts []string, format string) []*int64 {
	out := make([]*int64, len(texts))
	for i, text := range texts {
		if l := logs.ParseLines([]string{text}, format)[0]; l.Timed {
			unix := l.Unix
			out[i] = &unix
		}
	}
	return out
}

func unixOf(t *testing.T, text string) int64 {
	t.Helper()
	unix, ok := logs.ParseTime(text)
	if !ok {
		t.Fatalf("facts.json time %q doesn't read", text)
	}
	return unix
}

var runHeaderRE = regexp.MustCompile(`^=== run \S+ ===$`)
var nameDateRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// Per-line facts, read with the reader: format, line counts, own and borrowed
// times, run headers, lines stamped before the file's date, the window's edges.
func TestFactsPerLine(t *testing.T) {
	for _, fact := range loadFacts(t).Files {
		t.Run(path.Base(fact.Path), func(t *testing.T) {
			texts := readFactFile(t, fact.Path)
			if got := logs.Detect(texts); got != fact.Format {
				t.Errorf("format %q, want %q", got, fact.Format)
			}
			if len(texts) != fact.Lines {
				t.Errorf("%d lines, want %d", len(texts), fact.Lines)
			}
			own := ownTimes(texts, fact.Format)
			parsed := logs.ParseLines(texts, fact.Format)
			withOwn, before, atSince, atUntil := 0, 0, 0, 0
			var first, last *int64
			day, err := time.Parse("2006-01-02", nameDateRE.FindString(path.Base(fact.Path)))
			if err != nil {
				t.Fatalf("no date in the file name: %v", err)
			}
			for i, unix := range own {
				if unix == nil {
					continue
				}
				withOwn++
				if first == nil || *unix < *first {
					first = unix
				}
				if last == nil || *unix > *last {
					last = unix
				}
				if *unix < day.Unix() {
					before++
				}
				if fact.Window != nil && *unix == unixOf(t, fact.Window.Since) {
					atSince++
				}
				if fact.Window != nil && *unix == unixOf(t, fact.Window.Until) {
					atUntil++
				}
				if !parsed[i].Timed || parsed[i].Unix != *unix {
					t.Errorf("line %d: the reader's time differs from the line's own time", i+1)
				}
			}
			if withOwn != fact.LinesWithOwnTime || len(texts)-withOwn != fact.UntimedLines {
				t.Errorf("%d lines with their own time and %d without, want %d and %d",
					withOwn, len(texts)-withOwn, fact.LinesWithOwnTime, fact.UntimedLines)
			}
			if got := timeText(first); !sameTime(got, fact.FirstSeen) {
				t.Errorf("first own time %v, want %v", got, deref(fact.FirstSeen))
			}
			if got := timeText(last); !sameTime(got, fact.LastSeen) {
				t.Errorf("last own time %v, want %v", got, deref(fact.LastSeen))
			}
			if before != fact.LinesBeforeDate {
				t.Errorf("%d lines stamped before the file's date, want %d", before, fact.LinesBeforeDate)
			}
			if fact.RunHeaders != nil {
				headers, distinct := 0, map[string]bool{}
				for _, text := range texts {
					if runHeaderRE.MatchString(text) {
						headers++
						distinct[text] = true
					}
				}
				if headers != *fact.RunHeaders || len(distinct) != *fact.DistinctRunHeaders {
					t.Errorf("%d run headers, %d distinct; want %d and %d", headers, len(distinct), *fact.RunHeaders, *fact.DistinctRunHeaders)
				}
			}
			if fact.Window != nil {
				if atSince != fact.Window.LinesAtSince || atUntil != fact.Window.LinesAtUntil {
					t.Errorf("%d lines stamped at since and %d at until, want %d and %d",
						atSince, atUntil, fact.Window.LinesAtSince, fact.Window.LinesAtUntil)
				}
				since, until := unixOf(t, fact.Window.Since), unixOf(t, fact.Window.Until)
				inside := 0
				for _, l := range parsed {
					if l.Timed && l.Unix >= since && l.Unix < until {
						inside++
					}
				}
				if inside != fact.Window.LinesInWindow {
					t.Errorf("%d lines in the window, want %d", inside, fact.Window.LinesInWindow)
				}
			}
		})
	}
}

// Facts through the server's tools: counts, levels, times, the window and the top groups.
func TestFactsThroughTheTools(t *testing.T) {
	facts := loadFacts(t)
	s := startServer(t, bundleDir)
	for _, fact := range facts.Files {
		t.Run(path.Base(fact.Path), func(t *testing.T) {
			var out SummarizeLogsOutput
			s.call(t, "summarize_logs", map[string]any{"path": fact.Path, "min_level": "any", "limit": 3}, &out)
			if out.LinesScanned != fact.Lines || out.ByLevel != fact.ByLevel {
				t.Errorf("lines %d, by_level %+v; want %d, %+v", out.LinesScanned, out.ByLevel, fact.Lines, fact.ByLevel)
			}
			if !sameTime(deref(out.FirstSeen), fact.FirstSeen) || !sameTime(deref(out.LastSeen), fact.LastSeen) {
				t.Errorf("first_seen %v, last_seen %v; want %v, %v", deref(out.FirstSeen), deref(out.LastSeen), deref(fact.FirstSeen), deref(fact.LastSeen))
			}
			if len(out.Groups) != len(fact.TopGroups) {
				t.Fatalf("%d groups, want %d", len(out.Groups), len(fact.TopGroups))
			}
			for i, want := range fact.TopGroups {
				if out.Groups[i].GroupID != want.GroupID || out.Groups[i].Count != want.Count {
					t.Errorf("group %d: %s x%d, want %s x%d", i+1, out.Groups[i].GroupID, out.Groups[i].Count, want.GroupID, want.Count)
				}
			}
			if fact.Window != nil {
				var in SummarizeLogsOutput
				s.call(t, "summarize_logs", map[string]any{"path": fact.Path, "min_level": "any",
					"since": fact.Window.Since, "until": fact.Window.Until}, &in)
				if in.LinesInWindow != fact.Window.LinesInWindow {
					t.Errorf("lines_in_window %d, want %d", in.LinesInWindow, fact.Window.LinesInWindow)
				}
			}
			if fact.RunHeaders != nil {
				var found SearchLogsOutput
				s.call(t, "search_logs", map[string]any{"path": fact.Path, "pattern": `^=== run \S+ ===$`, "min_level": "any", "limit": 1}, &found)
				if found.Total != *fact.RunHeaders {
					t.Errorf("search_logs finds %d run headers, want %d", found.Total, *fact.RunHeaders)
				}
			}
		})
	}
}

// A .gz file and its decompressed copy summarize alike, apart from their paths.
func TestAGzFileReadsLikeItsPlainCopy(t *testing.T) {
	for _, rel := range loadFacts(t).GzVsPlain {
		t.Run(path.Base(rel), func(t *testing.T) {
			root := t.TempDir()
			name := path.Base(rel)
			src := filepath.Join(bundleDir, filepath.FromSlash(rel))
			copyFile(t, src, filepath.Join(root, "gz", name), false)
			copyFile(t, src, filepath.Join(root, "plain", strings.TrimSuffix(name, ".gz")), true)
			s := startServer(t, root)
			var gz, plain SummarizeLogsOutput
			s.call(t, "summarize_logs", map[string]any{"path": "gz", "min_level": "any", "limit": 100}, &gz)
			s.call(t, "summarize_logs", map[string]any{"path": "plain", "min_level": "any", "limit": 100}, &plain)
			for _, out := range []*SummarizeLogsOutput{&gz, &plain} {
				out.ScanID, out.ExpiresAt, out.Path = "", "", ""
				for i := range out.Groups {
					out.Groups[i].Files = nil
					out.Groups[i].Example.File = ""
				}
			}
			a, _ := json.Marshal(gz)
			b, _ := json.Marshal(plain)
			if string(a) != string(b) {
				t.Errorf("the plain copy summarizes differently:\ngz:    %.300s\nplain: %.300s", a, b)
			}
			if gz.LinesScanned == 0 {
				t.Error("the .gz file read as empty")
			}
		})
	}
}

func copyFile(t *testing.T, src, dst string, gunzip bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var r io.Reader = in
	if gunzip {
		zr, err := gzip.NewReader(in)
		if err != nil {
			t.Fatal(err)
		}
		defer zr.Close()
		r = zr
	}
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, r); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func timeText(unix *int64) string {
	if unix == nil {
		return ""
	}
	return logs.FormatTime(*unix)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sameTime(got string, want *string) bool {
	return got == deref(want)
}
