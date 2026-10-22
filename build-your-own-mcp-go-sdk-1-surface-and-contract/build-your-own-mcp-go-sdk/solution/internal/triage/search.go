package triage

import (
	"context"
	"fmt"
	"regexp"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

func searchLogsTool() *mcp.Tool {
	in := inputSchema[SearchLogsInput]()
	pathParam(in)
	pattern := in.Properties["pattern"]
	pattern.MinLength = jsonschema.Ptr(1)
	pattern.MaxLength = jsonschema.Ptr(200)
	windowParams(in)
	minLevelParam(in, "any")
	intParam(in, "limit", 1, 200, 50)
	intParam(in, "offset", 0, -1, 0)
	return &mcp.Tool{
		Name:  "search_logs",
		Title: "Search logs",
		Description: "Find the lines of a file or folder of the log root that match a regular expression, " +
			"optionally inside a time window and at or above a level. Returns each line with its file, " +
			"line number, time and level, in file and line order, a page at a time.",
		InputSchema: in,
		Annotations: readOnly("Search logs"),
	}
}

func (t *tools) searchLogs(_ context.Context, _ *mcp.CallToolRequest, in SearchLogsInput) (*mcp.CallToolResult, SearchLogsOutput, error) {
	target, err := t.root.Resolve(in.Path)
	if err != nil {
		return nil, SearchLogsOutput{}, err
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return nil, SearchLogsOutput{}, fmt.Errorf("pattern is not a valid regular expression: %v", err)
	}
	win, err := parseWindow(in.Since, in.Until)
	if err != nil {
		return nil, SearchLogsOutput{}, err
	}
	out := SearchLogsOutput{Path: target.Rel, Pattern: in.Pattern, Matches: []Match{}}
	var skipped []SkippedFile
	err = t.scan(target, &skipped, func(e logs.Entry, _ string, lines []logs.Line) {
		for _, l := range lines {
			if !win.contains(l) || !atOrAbove(l.Level, in.MinLevel) || !re.MatchString(l.Text) {
				continue
			}
			if out.Total >= in.Offset && out.Total < in.Offset+in.Limit {
				m := Match{File: e.Path, Line: l.Number, Level: l.Level, Text: logs.CutRunes(l.Text, lineTextLimit)}
				if l.Timed {
					m.Time = unixText(&l.Unix)
				}
				out.Matches = append(out.Matches, m)
			}
			out.Total++
		}
	})
	if err != nil {
		return nil, SearchLogsOutput{}, err
	}
	out.Shown = len(out.Matches)
	out.Note = pageNote(in.Offset, out.Shown, out.Total, "matches")
	return nil, out, nil
}
