package checks

// Task 6: --log-level, one stderr line per call at debug, stdout that stays protocol-only.

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const task6 = "Task 6 adds --log-level."

var callMark = regexp.MustCompile(`tool=([A-Za-z_]+)`)

func loggedFile() string {
	return factsDoc.GzVsPlain[0]
}

func loggedCalls() []rawRequest {
	file := loggedFile()
	return []rawRequest{
		{"tools/call", object{"name": "list_logs", "arguments": object{"path": file}}},
		{"tools/call", object{"name": "summarize_logs", "arguments": object{"path": file}}},
		{"tools/call", object{"name": "search_logs", "arguments": object{"path": file, "pattern": "quota", "limit": 1}}},
		{"tools/call", object{"name": "read_lines", "arguments": object{"path": file, "line": 3, "before": 1, "after": 1}}},
	}
}

// callLines counts the stderr lines that name a tool, by tool.
func callLines(stderr string) map[string]int {
	counts := map[string]int{}
	for _, line := range strings.Split(stderr, "\n") {
		if m := callMark.FindStringSubmatch(line); m != nil {
			counts[m[1]]++
		}
	}
	return counts
}

// exchange sends requests that must all succeed and returns the server's stderr.
func exchange(t *testing.T, requests []rawRequest, flags ...string) string {
	t.Helper()
	run := rawSession(t, s3Logs, filepath.Join(tempDir(t), "reports"), flags, requests...)
	for i, answer := range run.answers {
		result, isObject := answer["result"].(map[string]any)
		name := jsonScalar(requests[i].params["name"])
		if !isObject {
			t.Fatalf("%s %s failed: %s", requests[i].method, name, jsonText(answer["error"]))
		}
		if result["isError"] == true {
			t.Fatalf("%s returned a tool error: %s", name, cut(jsonText(result), 300))
		}
	}
	return run.stderr
}

func TestDebugWritesOneLinePerCall(t *testing.T) {
	stderr := exchange(t, loggedCalls(), "--log-level", "debug")
	got := callLines(stderr)
	want := map[string]int{"list_logs": 1, "summarize_logs": 1, "search_logs": 1, "read_lines": 1}
	if !maps.Equal(got, want) {
		t.Fatalf("at --log-level debug, every tools/call writes exactly one stderr line containing tool=<name> "+
			"(CONTRACT.md, Command line); for these four calls got %v. %s\nstderr:\n%s", got, task6, lastBytes(stderr, 1200))
	}
}

func TestARefusedCallStillWritesItsLine(t *testing.T) {
	refused := []rawRequest{{"tools/call", object{"name": "list_logs", "arguments": object{"path": loggedFile(), "surprise": 1}}}}
	run := rawSession(t, s3Logs, filepath.Join(tempDir(t), "reports"), []string{"--log-level", "debug"}, refused...)
	result, isObject := run.answers[0]["result"].(map[string]any)
	if isObject && result["isError"] != true {
		t.Fatal("list_logs with an unknown argument must be refused")
	}
	got := callLines(run.stderr)
	if want := map[string]int{"list_logs": 1}; !maps.Equal(got, want) {
		t.Fatalf("at --log-level debug, a call the schema refuses still writes exactly one tool=<name> line "+
			"(CONTRACT.md, Logging); got %v. %s\nstderr:\n%s", got, task6, lastBytes(run.stderr, 1200))
	}
}

func TestNoCallLinesAboveDebug(t *testing.T) {
	for _, c := range []struct {
		id    string
		flags []string
	}{
		{"default", nil},
		{"info", []string{"--log-level", "info"}},
		{"warning", []string{"--log-level", "warning"}},
	} {
		t.Run(c.id, func(t *testing.T) {
			stderr := exchange(t, loggedCalls(), c.flags...)
			if got := callLines(stderr); len(got) > 0 {
				level := strings.Join(c.flags, " ")
				if level == "" {
					level = "the default level"
				}
				t.Fatalf("with %s, no stderr line may contain tool=<name>; got %v. %s", level, got, task6)
			}
		})
	}
}

func TestStdoutStaysProtocolOnlyAtDebug(t *testing.T) {
	requests := append(loggedCalls(),
		rawRequest{"tools/list", object{}},
		rawRequest{"resources/read", object{"uri": "triage://formats"}},
		rawRequest{"tools/call", object{"name": "list_logs", "arguments": object{"path": loggedFile(), "surprise": 1}}},
	)
	run := rawSession(t, s3Logs, filepath.Join(tempDir(t), "reports"), []string{"--log-level", "debug"}, requests...)
	if len(run.answers) != len(requests) {
		t.Fatal("every request must get its own answer on stdout")
	}
}

func TestAnUnknownLevelExitsWithCode2(t *testing.T) {
	code, stdout, _ := runServer(t, "--root", s3Logs, "--log-level", "loud")
	if code != 2 {
		t.Fatalf("--log-level loud must exit with code 2, got %d. %s", code, task6)
	}
	if stdout != "" {
		t.Fatalf("nothing may go to stdout, got %q", cut(stdout, 200))
	}
}

func TestAMaskedErrorLeavesItsDetailOnStderr(t *testing.T) {
	base := tempDir(t)
	root := filepath.Join(base, "logs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "good.log"), []byte("2026-01-15T10:00:00.000Z INFO api request served\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad.log.gz"), []byte("this is not gzip data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := []rawRequest{{"tools/call", object{"name": "list_logs", "arguments": object{}}}}
	broken := append(append([]rawRequest{}, plain...),
		rawRequest{"tools/call", object{"name": "read_lines", "arguments": object{"path": "bad.log.gz", "line": 1}}})
	before := rawSession(t, root, filepath.Join(base, "r1"), nil, plain...)
	after := rawSession(t, root, filepath.Join(base, "r2"), nil, broken...)
	last, _ := after.answers[len(after.answers)-1]["result"].(map[string]any)
	if last["isError"] != true {
		t.Fatal("read_lines on a .gz file that isn't gzip must be a tool error")
	}
	if len(stderrLines(after.stderr)) <= len(stderrLines(before.stderr)) {
		t.Fatal("a masked unexpected error must write its detail to stderr (at error level, so it shows at every level)")
	}
}

// stderrLines splits stderr into lines, as Python's str.splitlines does for these texts.
func stderrLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func TestAnswersHoldAtDebug(t *testing.T) {
	file := loggedFile()
	running := ownServerFor(t, "Task 6", s3Logs, filepath.Join(tempDir(t), "reports"), "--log-level", "debug")
	var fact map[string]any
	for _, f := range factsDoc.Files {
		if f["path"] == file {
			fact = f
		}
	}
	result := ok(t, running.call(t, "summarize_logs", map[string]any{"path": file, "min_level": "any", "limit": 3}))
	if err := same(result["lines_scanned"], fact["lines"], "summarize_logs at --log-level debug: lines_scanned"); err != nil {
		t.Fatal(err)
	}
	if err := same(result["by_level"], fact["by_level"], "summarize_logs at --log-level debug: by_level"); err != nil {
		t.Fatal(err)
	}
}
