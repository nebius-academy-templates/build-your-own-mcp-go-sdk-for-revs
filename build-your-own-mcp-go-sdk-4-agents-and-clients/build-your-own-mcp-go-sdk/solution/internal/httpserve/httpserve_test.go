package httpserve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/triage"
)

const testTokens = "tok-a=analyst,tok-a2=analyst,tok-b=other,tok-v=viewer:read-only"

// startHTTP serves a server on a small made-up log root and returns its URL.
func startHTTP(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app", "api.log")
	if err := os.MkdirAll(filepath.Dir(app), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := "2026-01-15T10:00:00.000Z ERROR api upstream failed with code 502\n" +
		"2026-01-15T10:01:00.000Z ERROR api upstream failed with code 504\n"
	if err := os.WriteFile(app, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := paths.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := ParseTokens(testTokens)
	if err != nil {
		t.Fatal(err)
	}
	server := triage.NewServer(triage.Config{Root: root, Reports: t.TempDir(), ScanTTL: time.Minute})
	ts := httptest.NewServer(Handler(server, tokens))
	t.Cleanup(ts.Close)
	return ts.URL + Path
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// connect opens a client session that sends token with every request.
func connect(t *testing.T, url, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "httpserve-tests", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{token}}, MaxRetries: -1}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connecting with %s: %v", token, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestAScanBelongsToItsCaller(t *testing.T) {
	url := startHTTP(t)
	scanned := callTool(t, connect(t, url, "tok-a"), "summarize_logs", map[string]any{"path": "app"})
	result := scanned.StructuredContent.(map[string]any)
	scanID := result["scan_id"].(string)
	group := result["groups"].([]any)[0].(map[string]any)["group_id"].(string)
	args := map[string]any{"scan_id": scanID, "group_id": group}

	if res := callTool(t, connect(t, url, "tok-a2"), "group_lines", args); res.IsError {
		t.Errorf("the same principal's second token can't use the scan: %s", text(res))
	}
	res := callTool(t, connect(t, url, "tok-b"), "group_lines", args)
	want := `scan "` + scanID + `" is unknown or has expired. Call summarize_logs again for a new scan_id.`
	if !res.IsError || text(res) != want {
		t.Errorf("another principal: isError %v, %q; want E6", res.IsError, text(res))
	}
}

func TestAReadOnlyCallerCannotSave(t *testing.T) {
	url := startHTTP(t)
	viewer := connect(t, url, "tok-v")
	listed, err := viewer.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if slices.Contains(names, "save_finding") || !slices.Contains(names, "summarize_logs") {
		t.Errorf("the read-only caller's tools: %v", names)
	}
	finding := map[string]any{"title": "Upstream fails", "severity": "high", "summary": "Two failures.",
		"evidence": []any{map[string]any{"file": "app/api.log", "line": 1, "note": "the first"}}}
	res := callTool(t, viewer, "save_finding", finding)
	if !res.IsError || text(res) != "save_finding is not available to this caller." {
		t.Errorf("save_finding as the read-only caller: isError %v, %q; want E13", res.IsError, text(res))
	}
	if res := callTool(t, connect(t, url, "tok-a"), "save_finding", finding); res.IsError {
		t.Errorf("save_finding as a writing caller: %s", text(res))
	}
}

func TestTheOriginAndTokenChecks(t *testing.T) {
	url := startHTTP(t)
	for _, c := range []struct {
		name, origin, token string
		want                int
	}{
		{"no token", "", "", http.StatusUnauthorized},
		{"unknown token", "", "tok-x", http.StatusUnauthorized},
		{"foreign origin", "http://evil.example", "tok-a", http.StatusForbidden},
		{"foreign origin, no token", "https://localhost", "", http.StatusForbidden},
		{"local origin", "http://localhost:5173", "tok-a", http.StatusOK},
		{"upper-case scheme", "HTTP://localhost", "tok-a", http.StatusForbidden},
		{"upper-case host", "http://LOCALHOST", "tok-a", http.StatusForbidden},
		{"a path after the port", "http://127.0.0.1:8080/", "tok-a", http.StatusForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{` +
				`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
				`"io.modelcontextprotocol/clientInfo":{"name":"t","version":"0"},` +
				`"io.modelcontextprotocol/clientCapabilities":{}}}}`
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", "2026-07-28")
			req.Header.Set("Mcp-Method", "tools/list")
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, c.want)
			}
			if c.want == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate %q", resp.Header.Get("WWW-Authenticate"))
			}
		})
	}
}

func TestCheckAddress(t *testing.T) {
	for _, good := range []string{"127.0.0.1:8765", "localhost:1", "[::1]:65535", "127.0.0.1:0"} {
		if err := CheckAddress(good); err != nil {
			t.Errorf("CheckAddress(%q): %v", good, err)
		}
	}
	for _, bad := range []string{"127.0.0.1", ":8765", "127.0.0.1:", "127.0.0.1:notaport",
		"127.0.0.1:65536", "127.0.0.1:-1", "localhost:+80", "127.0.0.1:99999999999999999999"} {
		if err := CheckAddress(bad); err == nil {
			t.Errorf("CheckAddress(%q) accepted", bad)
		}
	}
}

func TestParseTokens(t *testing.T) {
	tokens, err := ParseTokens(testTokens)
	if err != nil {
		t.Fatal(err)
	}
	if tokens["tok-a2"] != (triage.Caller{Name: "analyst"}) || tokens["tok-v"] != (triage.Caller{Name: "viewer", ReadOnly: true}) {
		t.Errorf("tokens: %+v", tokens)
	}
	for _, bad := range []string{"", "tok-a", "=analyst", "tok-a=", "tok-a=analyst:admin"} {
		if _, err := ParseTokens(bad); err == nil {
			t.Errorf("ParseTokens(%q) accepted", bad)
		}
	}
}
