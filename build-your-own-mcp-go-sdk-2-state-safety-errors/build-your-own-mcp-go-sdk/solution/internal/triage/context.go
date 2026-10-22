package triage

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

func groupLinesTool() *mcp.Tool {
	in := inputSchema[GroupLinesInput]()
	in.Properties["scan_id"].Pattern = `^s_[0-9a-f]{16}$`
	in.Properties["group_id"].Pattern = `^[0-9a-f]{12}$`
	intParam(in, "limit", 1, 200, 50)
	intParam(in, "offset", 0, -1, 0)
	return &mcp.Tool{
		Name:  "group_lines",
		Title: "Show a group's lines",
		Description: "Page through the lines of one message group from an earlier summarize_logs call, " +
			"without scanning again: pass its scan_id and a group_id from its groups. Each line comes " +
			"with its file, line number and time.",
		InputSchema: in,
		Annotations: readOnly("Show a group's lines"),
	}
}

func (t *tools) groupLines(_ context.Context, _ *mcp.CallToolRequest, in GroupLinesInput) (*mcp.CallToolResult, GroupLinesOutput, error) {
	kept, found := t.scans.Get(localCaller, in.ScanID)
	if !found {
		return nil, GroupLinesOutput{}, errUnknownScan(in.ScanID)
	}
	g, found := kept.groups[in.GroupID]
	if !found {
		return nil, GroupLinesOutput{}, errGroupNotInScan(in.GroupID, in.ScanID)
	}
	page := pageOf(g.lines, in.Offset, in.Limit)
	texts, err := kept.readBack(t.root, page)
	if err != nil {
		return nil, GroupLinesOutput{}, err
	}
	out := GroupLinesOutput{
		ScanID: in.ScanID, GroupID: in.GroupID, Pattern: g.Pattern,
		Lines: []GroupLine{}, Total: g.Count, Shown: len(page),
	}
	for i, p := range page {
		line := GroupLine{File: kept.files[p.file].Path, Line: int(p.line), Text: logs.CutRunes(texts[i], lineTextLimit)}
		if p.timed {
			line.Time = unixText(&p.unix)
		}
		out.Lines = append(out.Lines, line)
	}
	out.Note = groupNote(in.Offset, out.Shown, len(g.lines), g.Count)
	return nil, out, nil
}

func readLinesTool() *mcp.Tool {
	in := inputSchema[ReadLinesInput]()
	in.Properties["path"].MaxLength = jsonschema.Ptr(500)
	in.Properties["line"].Minimum = jsonschema.Ptr(1.0)
	intParam(in, "before", 0, 50, 5)
	intParam(in, "after", 0, 200, 20)
	return &mcp.Tool{
		Name:  "read_lines",
		Title: "Read lines around a line",
		Description: "Read the lines around one line of one file under the log root: a few before it and " +
			"more after it, clipped to the file. Use it to check a line that summarize_logs, search_logs or " +
			"group_lines pointed at.",
		InputSchema: in,
		Annotations: readOnly("Read lines around a line"),
	}
}

func (t *tools) readLines(_ context.Context, _ *mcp.CallToolRequest, in ReadLinesInput) (*mcp.CallToolResult, ReadLinesOutput, error) {
	target, err := t.root.Resolve(in.Path)
	if err != nil {
		return nil, ReadLinesOutput{}, err
	}
	if target.IsDir {
		return nil, ReadLinesOutput{}, errFolder(target.Rel)
	}
	entries, err := t.root.Files(target)
	if err != nil {
		return nil, ReadLinesOutput{}, err
	}
	texts, err := logs.ReadLines(entries[0])
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("reading %s: %w", target.Rel, err)
	}
	total := len(texts)
	if in.Line > total {
		return nil, ReadLinesOutput{}, errPastTheEnd(in.Line, target.Rel, total)
	}
	start, end := max(in.Line-in.Before, 1), min(in.Line+in.After, total)
	out := ReadLinesOutput{File: target.Rel, Start: start, End: end, TotalLines: total, Lines: []TextLine{}}
	for n := start; n <= end; n++ {
		out.Lines = append(out.Lines, TextLine{Line: n, Text: logs.CutRunes(texts[n-1], lineTextLimit)})
	}
	return nil, out, nil
}

// groupNote pages against the kept lines and names the full count, and says
// when a group has more lines than a scan keeps.
func groupNote(offset, shown, kept, total int) string {
	note := ""
	if offset+shown < kept {
		note = fmt.Sprintf("Showing %d of %d lines. Pass offset=%d for more.", shown, total, offset+shown)
	}
	if total > kept {
		if note != "" {
			note += " "
		}
		note += fmt.Sprintf("Only the first %d lines are kept.", keptLines)
	}
	return note
}

// readBack returns the raw text of each position, read again from the files
// the scan read.
func (s *scan) readBack(root logs.Root, page []position) ([]string, error) {
	texts := make([]string, len(page))
	var lines []string
	current := int32(-1)
	for i, p := range page {
		if p.file != current {
			var err error
			if lines, err = root.ReadLines(s.files[p.file]); err != nil {
				return nil, fmt.Errorf("reading %s: %w", s.files[p.file].Path, err)
			}
			current = p.file
		}
		if int(p.line) <= len(lines) {
			texts[i] = lines[p.line-1]
		}
	}
	return texts, nil
}
