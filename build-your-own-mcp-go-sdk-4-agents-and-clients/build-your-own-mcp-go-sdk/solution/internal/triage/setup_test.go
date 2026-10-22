package triage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
)

// testFiles is a small made-up log root: an app log, an access log and a file
// of unknown format.
var testFiles = map[string]string{
	"app/api.log": strings.Join([]string{
		"2026-01-15T10:00:00.000Z INFO api request served in 12 ms",
		"2026-01-15T10:00:05.000Z WARN api request slow in 812 ms",
		"2026-01-15T10:01:00.000Z ERROR api upstream failed with code 502",
		"2026-01-15T10:02:00.000Z INFO api request served in 15 ms",
		"2026-01-15T10:03:00.000Z ERROR api upstream failed with code 504",
		"2026-01-15T10:04:00.000Z INFO api request served in 9 ms",
		"2026-01-15T11:00:00.000Z ERROR api upstream failed with code 502",
	}, "\n") + "\n",
	"web/access.log": strings.Join([]string{
		`192.0.2.10 - - [15/Jan/2026:10:00:00 +0000] "GET /home HTTP/1.1" 200 512 "-" "curl/8.5.0"`,
		`192.0.2.10 - - [15/Jan/2026:10:00:01 +0000] "GET /cart HTTP/1.1" 500 12 "-" "curl/8.5.0"`,
		`192.0.2.10 - - [15/Jan/2026:10:00:02 +0000] "GET /cart HTTP/1.1" 404 12 "-" "curl/8.5.0"`,
	}, "\n") + "\n",
	"notes/readme.txt": "a note that is not a log\n",
}

// testRoot writes testFiles into a new temporary folder and returns it.
func testRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range testFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// testServer holds a server connected in memory and the client talking to it.
type testServer struct {
	server  *mcp.Server
	session *mcp.ClientSession
	reports string
}

// startServer builds the server on root and connects a client to it in memory.
func startServer(t *testing.T, root string) *testServer {
	t.Helper()
	logRoot, err := paths.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(t.TempDir(), "reports")
	s := &testServer{server: NewServer(Config{Root: logRoot, Reports: reports, ScanTTL: time.Minute}), reports: reports}
	s.reconnect(t)
	return s
}

// reconnect gives the test a new client session on the same server. The client
// keeps resource reads for their ttlMs, so a test that reads a resource again
// after a change needs a new session.
func (s *testServer) reconnect(t *testing.T) {
	t.Helper()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := s.server.Connect(ctx, serverSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "triage-tests", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Wait()
	})
	s.session = session
}

// call runs a tool that must succeed and decodes its structured result into out.
func (s *testServer) call(t *testing.T, name string, args map[string]any, out any) {
	t.Helper()
	res, err := s.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a tool error: %s", name, resultText(res))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: the structured result doesn't decode: %v", name, err)
	}
}

// refusal runs a tool that must answer with a tool error and returns its text.
func (s *testServer) refusal(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	res, err := s.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s succeeded; it must be refused", name)
	}
	return resultText(res)
}

// read returns the text of a resource.
func (s *testServer) read(t *testing.T, uri string) string {
	t.Helper()
	res, err := s.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("reading %s: %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("reading %s gave %d contents", uri, len(res.Contents))
	}
	return res.Contents[0].Text
}

// asJSON writes v for a failure message, with pointer fields as their values.
func asJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func resultText(res *mcp.CallToolResult) string {
	var texts []string
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	return strings.Join(texts, " ")
}
