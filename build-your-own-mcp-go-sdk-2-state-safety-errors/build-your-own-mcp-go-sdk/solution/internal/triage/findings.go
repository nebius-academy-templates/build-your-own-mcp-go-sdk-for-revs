package triage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

const slugLimit = 60

func saveFindingTool() *mcp.Tool {
	in := inputSchema[SaveFindingInput]()
	p := in.Properties
	p["title"].MinLength, p["title"].MaxLength = jsonschema.Ptr(3), jsonschema.Ptr(120)
	p["severity"].Enum = []any{"low", "medium", "high", "critical"}
	p["summary"].MinLength, p["summary"].MaxLength = jsonschema.Ptr(1), jsonschema.Ptr(4000)
	evidence := p["evidence"]
	evidence.Type, evidence.Types = "array", nil
	evidence.MinItems, evidence.MaxItems = jsonschema.Ptr(1), jsonschema.Ptr(20)
	item := evidence.Items.Properties
	item["file"].MaxLength = jsonschema.Ptr(500)
	item["line"].Minimum = jsonschema.Ptr(1.0)
	item["note"].MinLength, item["note"].MaxLength = jsonschema.Ptr(1), jsonschema.Ptr(300)
	return &mcp.Tool{
		Name:  "save_finding",
		Title: "Save a finding",
		Description: "Save a conclusion as a markdown file in the reports folder: a title, a severity, a " +
			"summary and the log lines it relies on, each cited by file and line. The file is named " +
			"after the title. A name already in use is refused with E9, leaving the existing report unchanged.",
		InputSchema: in,
		Annotations: &mcp.ToolAnnotations{
			Title: "Save a finding", DestructiveHint: jsonschema.Ptr(false), OpenWorldHint: jsonschema.Ptr(false),
		},
	}
}

func (t *tools) saveFinding(_ context.Context, _ *mcp.CallToolRequest, in SaveFindingInput) (*mcp.CallToolResult, SaveFindingOutput, error) {
	name := slug(in.Title)
	file := filepath.Join(t.cfg.Reports, name+".md")
	if _, err := os.Lstat(file); err == nil {
		return nil, SaveFindingOutput{}, errExists(name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, SaveFindingOutput{}, err
	}
	for _, e := range in.Evidence {
		if err := t.checkEvidence(e); err != nil {
			return nil, SaveFindingOutput{}, err
		}
	}
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- Severity: %s\n- Saved: %s\n\n%s\n\n## Evidence\n\n", in.Title, in.Severity, created, in.Summary)
	for _, e := range in.Evidence {
		fmt.Fprintf(&b, "- `%s` line %d: %s\n", e.File, e.Line, e.Note)
	}
	if err := os.MkdirAll(t.cfg.Reports, 0o755); err != nil {
		return nil, SaveFindingOutput{}, err
	}
	if err := writeNew(file, b.String()); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, SaveFindingOutput{}, errExists(name)
		}
		return nil, SaveFindingOutput{}, err
	}
	return nil, SaveFindingOutput{Name: name, Path: name + ".md", Created: created}, nil
}

// checkEvidence refuses an evidence item whose file isn't one file under the
// log root or whose line isn't in that file.
func (t *tools) checkEvidence(e Evidence) error {
	target, err := t.root.Resolve(e.File)
	if err != nil {
		return err
	}
	if target.IsDir {
		return errFolder(target.Rel)
	}
	entries, err := t.root.Files(target)
	if err != nil {
		return err
	}
	lines, err := logs.ReadLines(entries[0])
	if err != nil {
		return fmt.Errorf("reading %s: %w", target.Rel, err)
	}
	if e.Line > len(lines) {
		return errPastTheEnd(e.Line, target.Rel, len(lines))
	}
	return nil
}

// writeNew writes content to a file that must not exist yet.
func writeNew(file, content string) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(file)
		return err
	}
	return f.Close()
}

// slug turns a title into a file name: ASCII letters lowercased, every other run
// of characters outside a-z and 0-9 made one "-", trimmed, cut to 60.
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range title {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > slugLimit {
		s = s[:slugLimit]
	}
	s = strings.TrimRight(s, "-")
	if s == "" {
		return "finding"
	}
	return s
}
