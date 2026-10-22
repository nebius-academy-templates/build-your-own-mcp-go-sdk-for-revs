package checks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/5-telemetry/checks/internal/receiver"
)

func TestT3Reading(t *testing.T) {
	s := start(t, "stdio", true, nil)
	scan := structured(t, s.call("summarize_logs", map[string]any{"min_level": "any"}, nil))
	groups, ok := scan["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatal("synthetic app logs must produce groups")
	}
	group := groups[0].(map[string]any)
	s.call("group_lines", map[string]any{"scan_id": scan["scan_id"], "group_id": group["group_id"]}, nil)
	rows := s.stop("eof")
	summary := find(t, rows, "tools/call summarize_logs")
	groupCall := find(t, rows, "tools/call group_lines")
	var files, lines, readBytes, reusedBytes int64
	var reads int
	for _, r := range rows {
		parent := receiver.ID(r.Span.ParentSpanId)
		if parent == receiver.ID(summary.Span.SpanId) {
			if r.Span.Name == "logtriage.read" {
				reads++
				equal(t, int32(r.Span.Kind), int32(1))
				files += integer(t, attrs(r)["logtriage.files"])
				lines += integer(t, attrs(r)["logtriage.lines"])
				readBytes += integer(t, attrs(r)["logtriage.bytes"])
			}
			if receiver.ID(r.Span.TraceId) != receiver.ID(summary.Span.TraceId) {
				t.Error("inner span must be in the request trace")
			}
		}
		if parent == receiver.ID(groupCall.Span.SpanId) && r.Span.Name == "logtriage.read" {
			reusedBytes += integer(t, attrs(r)["logtriage.bytes"])
		}
	}
	plain := "2040-01-01T00:00:00.000Z ERROR worker request 1 failed\n2040-01-01T00:00:01.000Z INFO worker ready\n"
	equalWhy(t, reads, 4, "summarize_logs over two files needs 4 logtriage.read children (detection and content)")
	equalWhy(t, files, 4, "logtriage.files summed over the read spans")
	equalWhy(t, lines, 8, "logtriage.lines summed over the read spans")
	equalWhy(t, readBytes, 4*len(plain), "logtriage.bytes summed over the read spans")
	if reusedBytes <= 0 || reusedBytes >= readBytes {
		t.Errorf("stored group read bytes=%d must be positive and below full scan=%d", reusedBytes, readBytes)
	}
}
func integer(t *testing.T, value any) int64 {
	t.Helper()
	v, ok := value.(int64)
	if !ok {
		t.Fatalf("expected integer span attribute, got %v", value)
	}
	return v
}
func TestT3NoReadForRefusedPath(t *testing.T) {
	s := start(t, "stdio", true, nil)
	s.call("read_lines", map[string]any{"path": "../forbidden", "line": 1}, nil)
	rows := s.stop("eof")
	find(t, rows, "tools/call read_lines")
	for _, r := range rows {
		if r.Span.Name == "logtriage.read" {
			t.Error("no read span when no file was opened")
		}
	}
}

// longLog is 400 short app-log lines, about 20 KB: more than a format detection needs.
func longLog() string {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "2040-01-01T00:%02d:%02d.000Z INFO worker step %d done\n", i/60, i%60, i)
	}
	return b.String()
}

// records counts the newline records in data, and a final nonempty unterminated one.
func records(data []byte) int64 {
	n := int64(bytes.Count(data, []byte("\n")))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// TestT3LinesCountWhatWasRead: logtriage.lines counts the records in the bytes a reader returned,
// read-ahead included, not the lines the parser took from them. list_logs detects the format,
// which opens a reader.
func TestT3LinesCountWhatWasRead(t *testing.T) {
	s := start(t, "stdio", true, nil)
	content := []byte(longLog())
	if err := os.MkdirAll(filepath.Join(s.root, "long"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "long", "gamma.log"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	s.call("list_logs", map[string]any{"path": "long"}, nil)
	rows := s.stop("eof")
	reads := 0
	for _, r := range rows {
		if r.Span.Name != "logtriage.read" {
			continue
		}
		reads++
		read := integer(t, attrs(r)["logtriage.bytes"])
		lines := integer(t, attrs(r)["logtriage.lines"])
		end := min(read, int64(len(content)))
		equalWhy(t, lines, records(content[:end]), fmt.Sprintf("logtriage.lines must count the records in the %d bytes read", read))
	}
	if reads == 0 {
		t.Fatal("list_logs detects the file's format, which opens a reader: no logtriage.read span")
	}
}
