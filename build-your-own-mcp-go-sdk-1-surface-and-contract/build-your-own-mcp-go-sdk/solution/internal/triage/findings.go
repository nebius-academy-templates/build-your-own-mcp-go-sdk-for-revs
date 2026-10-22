package triage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
			"after the title and overwrites an existing report with the same name.",
		InputSchema: in,
		Annotations: &mcp.ToolAnnotations{
			Title: "Save a finding", DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(false),
		},
	}
}

func (t *tools) saveFinding(_ context.Context, _ *mcp.CallToolRequest, in SaveFindingInput) (*mcp.CallToolResult, SaveFindingOutput, error) {
	name := slug(in.Title)
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- Severity: %s\n- Saved: %s\n\n%s\n\n## Evidence\n\n", in.Title, in.Severity, created, in.Summary)
	for _, e := range in.Evidence {
		fmt.Fprintf(&b, "- `%s` line %d: %s\n", e.File, e.Line, e.Note)
	}
	if err := os.MkdirAll(t.cfg.Reports, 0o755); err != nil {
		return nil, SaveFindingOutput{}, err
	}
	if err := os.WriteFile(filepath.Join(t.cfg.Reports, name+".md"), []byte(b.String()), 0o644); err != nil {
		return nil, SaveFindingOutput{}, err
	}
	return nil, SaveFindingOutput{Name: name, Path: name + ".md", Created: created}, nil
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
