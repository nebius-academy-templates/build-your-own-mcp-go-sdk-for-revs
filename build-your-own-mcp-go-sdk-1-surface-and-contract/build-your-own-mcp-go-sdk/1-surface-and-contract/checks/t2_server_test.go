package checks

// Task 2: the server object, its command line and stdio.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const instructions = "Log Triage reads log files under one folder. Start with list_logs to see what is there. " +
	"Use summarize_logs on a file or folder, with since and until when you know the time, to get " +
	"counts and message groups instead of raw lines. Use search_logs to find the lines that match a " +
	"pattern. Record a conclusion with save_finding and cite a file and line for every piece of " +
	"evidence. All times are UTC."

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func versionTuple(t *testing.T, text string) [3]int {
	t.Helper()
	m := versionRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("the server version must look like 0.1.0, got %q", text)
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v
}

func atLeast(v, min [3]int) bool {
	return slices.Compare(v[:], min[:]) >= 0
}

type runResult struct {
	code   int
	stdout string
	stderr string
}

// runOnce runs the server command with args, stdin closed, and waits for it to exit.
func runOnce(t *testing.T, args ...string) runResult {
	t.Helper()
	base, tried, err := serverCommand()
	if err != nil {
		t.Fatal(startFailure(tried, err.Error(), "(nothing)"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, base[0], append(append([]string{}, base[1:]...), args...)...)
	cmd.Dir = checksDir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "VIRTUAL_ENV=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("could not run %s: %v", shellJoin(cmd.Args), err)
	}
	if ctx.Err() != nil {
		t.Fatalf("%s did not exit within 180 s", shellJoin(cmd.Args))
	}
	return runResult{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

func stderrLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func TestServerStartsOverStdio(t *testing.T) {
	s := logServer(t)
	s.require(t)
	if s.protocolVersion(t) == "" {
		t.Fatal("the server connected but no protocol version was agreed")
	}
}

func TestServerNameIsLogTriage(t *testing.T) {
	if name := logServer(t).serverInfo(t).Name; name != "log-triage" {
		t.Fatalf("the server name must be 'log-triage', got %q", name)
	}
}

func TestServerVersionIsAtLeast010(t *testing.T) {
	version := logServer(t).serverInfo(t).Version
	if !atLeast(versionTuple(t, version), [3]int{0, 1, 0}) {
		t.Fatalf("the server version must be 0.1.0 or later, got %q", version)
	}
}

func TestInstructionsMatchTheContract(t *testing.T) {
	s := logServer(t)
	text := s.instructions(t)
	if text == "" {
		t.Fatal("the server publishes no instructions; CONTRACT.md, Instructions, gives the text")
	}
	if !atLeast(versionTuple(t, s.serverInfo(t).Version), [3]int{0, 2, 0}) {
		if n := len([]rune(text)); n > 512 {
			t.Fatalf("instructions must be at most 512 characters, got %d", n)
		}
		if text != instructions {
			t.Fatalf("instructions must be exactly the text in CONTRACT.md, Instructions.\nexpected: %q\ngot:      %q",
				instructions, text)
		}
	} else if !strings.HasPrefix(text, instructions) {
		t.Fatal("later instructions must keep the practice 1 text first")
	}
}

func TestMissingRootExitsWithCode2(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no such folder")
	run := runOnce(t, "--root", missing)
	line := `log root "` + missing + `" is not a readable folder`
	if !slices.Contains(stderrLines(run.stderr), line) {
		t.Fatalf("with a missing --root the server must print this line on stderr:\n%s\nstderr was:\n%s",
			line, tail(run.stderr, 800))
	}
	if run.code != 2 {
		t.Fatalf("with a missing --root the exit code must be 2, got %d", run.code)
	}
	if run.stdout != "" {
		t.Fatalf("nothing may go to stdout, got %q", cut(run.stdout, 200))
	}
}

func TestRootThatIsAFileExitsWithCode2(t *testing.T) {
	aFile := filepath.Join(t.TempDir(), "not a folder.log")
	if err := os.WriteFile(aFile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := runOnce(t, "--root", aFile)
	line := `log root "` + aFile + `" is not a readable folder`
	if !slices.Contains(stderrLines(run.stderr), line) {
		t.Fatalf("with a file as --root the server must print this line on stderr:\n%s\nstderr was:\n%s",
			line, tail(run.stderr, 800))
	}
	if run.code != 2 {
		t.Fatalf("with a file as --root the exit code must be 2, got %d", run.code)
	}
}

func TestServerAcceptsReportsAndReadOnly(t *testing.T) {
	s := ownServer(t, s3Logs, filepath.Join(t.TempDir(), "reports"), "--read-only")
	s.require(t)
	s.toolList(t)
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
