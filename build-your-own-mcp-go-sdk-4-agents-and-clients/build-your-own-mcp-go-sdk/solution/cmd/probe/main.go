// Command probe is a scripted client that behaves like a careless host
// (3-testing-and-debugging/probe-expected.md). It starts the server over stdio
// with --scan-ttl 1, sends five requests in order and prints one line per
// request. It exits 0 when every step got the answer the page names.
//
//	go -C solution run ./cmd/probe --root ../s3_logs
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	madeUpScan  = "s_0000000000000000"
	madeUpGroup = "000000000000"
	hostilePath = "../outside.log"
)

var scanOutcome = regexp.MustCompile(`^ok scan_id=s_[0-9a-f]{16}$`)

func e1(path string) string {
	return `path "` + path + `" is outside the log root. Pass a path relative to the log root; ` +
		`list_logs with path "." shows what is there.`
}

func e6(scanID string) string {
	return `scan "` + scanID + `" is unknown or has expired. Call summarize_logs again for a new scan_id.`
}

// contractErrors match the error texts of CONTRACT.md section 5, E1 to E12.
// In a template, <v> stands for a quoted value and <n> for a number.
var contractErrors = compileTemplates(
	`path "<v>" is outside the log root. Pass a path relative to the log root; list_logs with path "." shows what is there.`,
	`path "<v>" does not exist under the log root. Call list_logs to see what is there.`,
	`since must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z.`,
	`until must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z.`,
	`until must be later than since.`,
	`pattern is not a valid regular expression. Escape special characters such as ( [ . * with a backslash, or search for a plain word.`,
	`value_pattern is not a valid regular expression. Escape special characters such as ( [ . * with a backslash, or search for a plain word.`,
	`scan "<v>" is unknown or has expired. Call summarize_logs again for a new scan_id.`,
	`group "<v>" is not in scan "<v>". Use a group_id from that scan's groups.`,
	`line <n> is past the end of "<v>", which has <n> lines.`,
	`a finding named "<v>" already exists. Choose a different title.`,
	`"<v>" is a folder. This needs one file; call list_logs to pick one.`,
	`value_pattern must contain exactly one capture group, for example "size=(\d+)".`,
	`the window needs <n> buckets and the limit is 500. Pass a larger bucket_minutes.`,
)

func compileTemplates(templates ...string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, t := range templates {
		expr := regexp.QuoteMeta(t)
		expr = strings.ReplaceAll(expr, "<v>", "(?s:.*)")
		expr = strings.ReplaceAll(expr, "<n>", "[0-9]+")
		out = append(out, regexp.MustCompile("^"+expr+"$"))
	}
	return out
}

func isContractError(text string) bool {
	for _, re := range contractErrors {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func main() {
	root := flag.String("root", "", "the log root to start the server with")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "probe: --root is required")
		os.Exit(2)
	}
	passed, err := probe(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe:", err)
		os.Exit(1)
	}
	if !passed {
		os.Exit(1)
	}
}

// answer is what one tools/call came back with: a result, or the JSON-RPC
// error that refused it.
type answer struct {
	result  *mcp.CallToolResult
	refusal *jsonrpc.Error
}

func call(ctx context.Context, session *mcp.ClientSession, tool string, args map[string]any) (answer, error) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		return answer{refusal: rpcErr}, nil
	}
	if err != nil {
		return answer{}, fmt.Errorf("%s: %w", tool, err)
	}
	return answer{result: res}, nil
}

// describe turns an answer into the outcome part of a step line. An error
// result is a tool error only when its text is a contract error text: go-sdk
// answers a schema violation with the same shape, isError and no
// structuredContent, and its own text.
func describe(a answer) string {
	switch {
	case a.refusal != nil:
		return "refused"
	case a.result.IsError && isContractError(textOf(a.result)):
		return "tool error: " + textOf(a.result)
	case a.result.IsError:
		return "refused"
	}
	if id, _ := structured(a.result)["scan_id"].(string); id != "" {
		return "ok scan_id=" + id
	}
	return "ok"
}

func textOf(res *mcp.CallToolResult) string {
	text := ""
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			if text != "" {
				text += " "
			}
			text += t.Text
		}
	}
	return text
}

func structured(res *mcp.CallToolResult) map[string]any {
	if res == nil {
		return nil
	}
	m, _ := res.StructuredContent.(map[string]any)
	return m
}

func probe(rootArg string) (bool, error) {
	root, err := filepath.Abs(rootArg)
	if err != nil {
		return false, err
	}
	reports, err := os.MkdirTemp("", "log-triage-probe-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(reports)

	ctx := context.Background()
	server := exec.Command("go", "run", "./cmd/log-triage",
		"--root", root, "--scan-ttl", "1", "--reports", reports)
	server.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "log-triage-probe", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: server}, nil)
	if err != nil {
		return false, fmt.Errorf("starting the server: %w", err)
	}
	defer session.Close()

	passed := true
	report := func(step int, name, tool, outcome string, ok bool) {
		fmt.Printf("step %d %s: %s -> %s\n", step, name, tool, outcome)
		passed = passed && ok
	}

	a, err := call(ctx, session, "group_lines", map[string]any{"scan_id": madeUpScan, "group_id": madeUpGroup})
	if err != nil {
		return false, err
	}
	outcome := describe(a)
	report(1, "out-of-order", "group_lines", outcome, outcome == "tool error: "+e6(madeUpScan))

	listing, err := call(ctx, session, "list_logs", map[string]any{"limit": 1})
	if err != nil {
		return false, err
	}
	files, _ := structured(listing.result)["files"].([]any)
	if len(files) == 0 {
		return false, fmt.Errorf("list_logs found no file under %s", root)
	}
	first, _ := files[0].(map[string]any)["path"].(string)
	scanned, err := call(ctx, session, "summarize_logs", map[string]any{"path": first, "min_level": "any"})
	if err != nil {
		return false, err
	}
	outcome = describe(scanned)
	report(2, "scan", "summarize_logs", outcome, scanOutcome.MatchString(outcome))

	scanID, _ := structured(scanned.result)["scan_id"].(string)
	if scanID == "" {
		scanID = madeUpScan
	}
	time.Sleep(2 * time.Second)
	a, err = call(ctx, session, "group_lines", map[string]any{"scan_id": scanID, "group_id": madeUpGroup})
	if err != nil {
		return false, err
	}
	outcome = describe(a)
	report(3, "expired", "group_lines", outcome, outcome == "tool error: "+e6(scanID))

	a, err = call(ctx, session, "list_logs", map[string]any{"path": ".", "surprise": 1})
	if err != nil {
		return false, err
	}
	outcome = describe(a)
	report(4, "unknown-argument", "list_logs", outcome, outcome == "refused")

	a, err = call(ctx, session, "read_lines", map[string]any{"path": hostilePath, "line": 1})
	if err != nil {
		return false, err
	}
	outcome = describe(a)
	report(5, "hostile-path", "read_lines", outcome, outcome == "tool error: "+e1(hostilePath))

	return passed, nil
}
