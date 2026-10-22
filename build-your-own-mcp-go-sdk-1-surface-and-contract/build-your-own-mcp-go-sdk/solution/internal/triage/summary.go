package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

const (
	lineTextLimit  = 500
	groupFileLimit = 5
	skippedLimit   = 20
)

var minLevels = []any{"any", logs.LevelDebug, logs.LevelInfo, logs.LevelWarning, logs.LevelError, logs.LevelCritical}

func summarizeLogsTool() *mcp.Tool {
	in := inputSchema[SummarizeLogsInput]()
	pathParam(in)
	windowParams(in)
	minLevelParam(in, "warning")
	intParam(in, "limit", 1, 100, 20)
	return &mcp.Tool{
		Name:  "summarize_logs",
		Title: "Summarize logs",
		Description: "Summarize a file or folder of the log root over an optional time window: lines per " +
			"level and message groups (lines with the same shape), most frequent first, each with its " +
			"count, first and last time, files and one example line.",
		InputSchema: in,
		Annotations: readOnly("Summarize logs"),
	}
}

func (t *tools) summarizeLogs(_ context.Context, _ *mcp.CallToolRequest, in SummarizeLogsInput) (*mcp.CallToolResult, SummarizeLogsOutput, error) {
	target, err := t.root.Resolve(in.Path)
	if err != nil {
		return nil, SummarizeLogsOutput{}, err
	}
	win, err := parseWindow(in.Since, in.Until)
	if err != nil {
		return nil, SummarizeLogsOutput{}, err
	}
	out := SummarizeLogsOutput{
		Path: target.Rel, Since: win.sinceText(), Until: win.untilText(), MinLevel: in.MinLevel,
		Skipped: []SkippedFile{}, Groups: []Group{},
	}
	counts := map[string]int{}
	var first, last timeSpan
	byKey := map[string]*groupState{}
	var order []*groupState
	patterns := patternCache{}
	err = t.scan(target, &out.Skipped, func(e logs.Entry, format string, lines []logs.Line) {
		out.FilesScanned++
		out.LinesScanned += len(lines)
		for _, l := range lines {
			if !win.contains(l) {
				continue
			}
			out.LinesInWindow++
			counts[l.Level]++
			if l.Timed {
				first.add(l.Unix)
				last.add(l.Unix)
			}
			if !atOrAbove(l.Level, in.MinLevel) {
				continue
			}
			pattern := patterns.get(l.Message)
			key := format + "\x00" + l.Level + "\x00" + pattern
			g := byKey[key]
			if g == nil {
				g = &groupState{Group: Group{
					GroupID: logs.GroupID(format, l.Level, pattern), Format: format, Level: l.Level, Pattern: pattern,
					Files:   []string{},
					Example: Example{File: e.Path, Line: l.Number, Text: logs.CutRunes(l.Text, lineTextLimit)},
				}}
				byKey[key] = g
				order = append(order, g)
			}
			g.add(e.Path, l)
		}
	})
	if err != nil {
		return nil, SummarizeLogsOutput{}, err
	}
	out.FirstSeen, out.LastSeen = first.minText(), last.maxText()
	out.ByLevel = LevelCounts{
		Critical: counts[logs.LevelCritical], Error: counts[logs.LevelError], Warning: counts[logs.LevelWarning],
		Info: counts[logs.LevelInfo], Debug: counts[logs.LevelDebug], Unknown: counts[logs.LevelUnknown],
	}
	sort.Slice(order, func(i, j int) bool { return order[i].before(order[j]) })
	out.TotalGroups = len(order)
	for _, g := range pageOf(order, 0, in.Limit) {
		out.Groups = append(out.Groups, g.result())
	}
	out.Shown = len(out.Groups)
	if out.Shown < out.TotalGroups {
		out.Note = fmt.Sprintf("Showing %d of %d groups. Raise limit (up to 100) or narrow the path or the window.",
			out.Shown, out.TotalGroups)
	}
	return nil, out, nil
}

// groupState is a group while the scan still adds lines to it.
type groupState struct {
	Group
	span     timeSpan
	lastFile string
}

func (g *groupState) add(file string, l logs.Line) {
	g.Count++
	if l.Timed {
		g.span.add(l.Unix)
	}
	if file != g.lastFile {
		g.lastFile = file
		g.FileCount++
		if len(g.Files) < groupFileLimit {
			g.Files = append(g.Files, file)
		}
	}
}

// before orders groups by count, highest first, then first time, earliest
// first and untimed last, then group_id.
func (g *groupState) before(o *groupState) bool {
	if g.Count != o.Count {
		return g.Count > o.Count
	}
	if g.span.set != o.span.set {
		return g.span.set
	}
	if g.span.set && g.span.min != o.span.min {
		return g.span.min < o.span.min
	}
	return g.GroupID < o.GroupID
}

func (g *groupState) result() Group {
	out := g.Group
	out.FirstSeen, out.LastSeen = g.span.minText(), g.span.maxText()
	return out
}

// timeSpan is the earliest and latest of a set of times.
type timeSpan struct {
	set      bool
	min, max int64
}

func (s *timeSpan) add(unix int64) {
	if !s.set {
		s.set, s.min, s.max = true, unix, unix
		return
	}
	s.min = min(s.min, unix)
	s.max = max(s.max, unix)
}

func (s timeSpan) minText() *string { return s.text(s.min) }
func (s timeSpan) maxText() *string { return s.text(s.max) }

func (s timeSpan) text(unix int64) *string {
	if !s.set {
		return nil
	}
	t := logs.FormatTime(unix)
	return &t
}

// patternCache remembers the pattern of messages seen in one scan.
type patternCache map[string]string

func (c patternCache) get(message string) string {
	if p, ok := c[message]; ok {
		return p
	}
	if len(c) >= 200_000 {
		clear(c)
	}
	p := logs.Pattern(message)
	c[message] = p
	return p
}

// ---------------------------------------------------------------- shared by the scanning tools

// scan reads every file the target stands for, in byte order, and hands each
// readable file's lines to fn. Files of unknown format are added to skipped.
func (t *tools) scan(target logs.Target, skipped *[]SkippedFile, fn func(logs.Entry, string, []logs.Line)) error {
	entries, err := t.root.Files(target)
	if err != nil {
		return err
	}
	for _, e := range entries {
		texts, err := logs.ReadLines(e)
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Path, err)
		}
		format := logs.Detect(texts)
		if format == logs.FormatUnknown {
			if len(*skipped) < skippedLimit {
				*skipped = append(*skipped, SkippedFile{Path: e.Path, Reason: "unknown format"})
			}
			continue
		}
		fn(e, format, logs.ParseLines(texts, format))
	}
	return nil
}

// window is since <= time < until; a missing end is open.
type window struct {
	since, until *int64
}

func parseWindow(since, until *string) (window, error) {
	var w window
	for _, end := range []struct {
		name string
		text *string
		into **int64
	}{{"since", since, &w.since}, {"until", until, &w.until}} {
		if end.text == nil {
			continue
		}
		unix, ok := logs.ParseTime(*end.text)
		if !ok {
			return window{}, fmt.Errorf("%s must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z.", end.name)
		}
		*end.into = &unix
	}
	return w, nil
}

// contains says whether a line is in the window. With no end given every line
// is, untimed ones included; with an end given an untimed line isn't.
func (w window) contains(l logs.Line) bool {
	if w.since == nil && w.until == nil {
		return true
	}
	if !l.Timed {
		return false
	}
	return (w.since == nil || l.Unix >= *w.since) && (w.until == nil || l.Unix < *w.until)
}

func (w window) sinceText() *string { return unixText(w.since) }
func (w window) untilText() *string { return unixText(w.until) }

func unixText(unix *int64) *string {
	if unix == nil {
		return nil
	}
	t := logs.FormatTime(*unix)
	return &t
}

// atOrAbove says whether a line's level passes min_level; "any" passes every
// level, unknown included.
func atOrAbove(level, minLevel string) bool {
	if minLevel == "any" {
		return true
	}
	return level != logs.LevelUnknown && logs.LevelRank(level) >= logs.LevelRank(minLevel)
}

func windowParams(s *jsonschema.Schema) {
	for _, name := range []string{"since", "until"} {
		p := s.Properties[name]
		p.MaxLength = jsonschema.Ptr(40)
		p.Default = json.RawMessage(`null`)
	}
}

func minLevelParam(s *jsonschema.Schema, def string) {
	p := s.Properties["min_level"]
	p.Enum = minLevels
	p.Default = json.RawMessage(fmt.Sprintf("%q", def))
}
