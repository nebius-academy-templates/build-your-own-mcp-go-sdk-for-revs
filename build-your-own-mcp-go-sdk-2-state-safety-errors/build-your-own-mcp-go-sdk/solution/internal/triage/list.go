package triage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

func listLogsTool() *mcp.Tool {
	in := inputSchema[ListLogsInput]()
	pathParam(in)
	intParam(in, "limit", 1, 500, 50)
	intParam(in, "offset", 0, -1, 0)
	return &mcp.Tool{
		Name:  "list_logs",
		Title: "List log files",
		Description: "List the log files under a file or folder of the log root: each file's path, size, " +
			"whether it is gzip, and its detected format. Start here to see what there is to read; a limit " +
			"of up to 500 lists a whole folder in one call.",
		InputSchema: in,
		Annotations: readOnly("List log files"),
	}
}

func (t *tools) listLogs(_ context.Context, _ *mcp.CallToolRequest, in ListLogsInput) (*mcp.CallToolResult, ListLogsOutput, error) {
	target, err := t.root.Resolve(in.Path)
	if err != nil {
		return nil, ListLogsOutput{}, err
	}
	found, err := t.root.Files(target)
	if err != nil {
		return nil, ListLogsOutput{}, err
	}
	var entries []logs.Entry
	for _, e := range found {
		if !e.Symlink {
			entries = append(entries, e)
		}
	}
	page := pageOf(entries, in.Offset, in.Limit)
	out := ListLogsOutput{Path: target.Rel, Files: []LogFile{}, Total: len(entries), Shown: len(page)}
	for _, e := range page {
		format, err := logs.DetectFile(e)
		if errors.Is(err, logs.ErrUnreadable) {
			format, err = logs.FormatUnknown, nil
		}
		if err != nil {
			return nil, ListLogsOutput{}, err
		}
		out.Files = append(out.Files, LogFile{Path: e.Path, SizeBytes: e.Size, Compressed: e.Compressed, Format: format})
	}
	out.Note = pageNote(in.Offset, out.Shown, out.Total, "files")
	return nil, out, nil
}

// pageOf returns items[offset:offset+limit], clipped to the slice.
func pageOf[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	end := len(items)
	if offset+limit < end {
		end = offset + limit
	}
	return items[offset:end]
}

// pageNote is "" when no later page exists, and says how to get it otherwise.
func pageNote(offset, shown, total int, things string) string {
	if offset+shown >= total {
		return ""
	}
	return fmt.Sprintf("Showing %d of %d %s. Pass offset=%d for more.", shown, total, things, offset+shown)
}

// ---------------------------------------------------------------- schemas

// inputSchema infers the schema of a tool's arguments; the caller adds bounds
// and defaults.
func inputSchema[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	return s
}

func pathParam(s *jsonschema.Schema) {
	p := s.Properties["path"]
	p.MaxLength = jsonschema.Ptr(500)
	p.Default = json.RawMessage(`"."`)
}

// intParam sets the bounds and default of an integer parameter; a negative max
// means no upper bound.
func intParam(s *jsonschema.Schema, name string, min, max, def int) {
	p := s.Properties[name]
	p.Minimum = jsonschema.Ptr(float64(min))
	if max >= 0 {
		p.Maximum = jsonschema.Ptr(float64(max))
	}
	p.Default = json.RawMessage(fmt.Sprint(def))
}

// readOnly is the annotation set of a tool that only reads the log root.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: jsonschema.Ptr(false)}
}
