package checks

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/5-telemetry/checks/internal/receiver"
)

var command []string
var buildErr error
var buildOnce sync.Once
var binaryDir string

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryDir != "" {
		if err := os.RemoveAll(binaryDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

type safeBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *safeBuffer) text() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }

type server struct {
	t              *testing.T
	cmd            *exec.Cmd
	in             io.WriteCloser
	lines          chan []byte
	done           chan error
	stderr, stdout safeBuffer
	url            string
	next           int
	stopped        bool
	receiver       *receiver.Receiver
	root, reports  string
}

func start(t *testing.T, transport string, enabled bool, extra map[string]string) *server {
	t.Helper()
	buildOnce.Do(func() {
		if raw := os.Getenv("LOG_TRIAGE_CMD"); raw != "" {
			buildErr = json.Unmarshal([]byte(raw), &command)
			if buildErr == nil && len(command) == 0 {
				buildErr = fmt.Errorf("empty LOG_TRIAGE_CMD")
			}
			return
		}
		cwd, err := os.Getwd()
		if err != nil {
			buildErr = err
			return
		}
		buildDir, err := os.MkdirTemp("", "p5-check-binary-")
		if err != nil {
			buildErr = err
			return
		}
		binaryDir = buildDir
		bin := filepath.Join(buildDir, "log-triage")
		out, err := exec.Command("go", "-C", filepath.Join(cwd, "../../solution"), "build", "-o", bin, "./cmd/log-triage").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build server: %w\n%s", err, out)
			return
		}
		command = []string{bin}
	})
	if buildErr != nil {
		t.Fatalf("server required before telemetry tasks: %v", buildErr)
	}
	s := &server{t: t, root: t.TempDir(), reports: t.TempDir(), receiver: &receiver.Receiver{}, lines: make(chan []byte, 100), done: make(chan error, 1)}
	// Independent tiny synthetic logs, not any investigation's evidence.
	plain := "2040-01-01T00:00:00.000Z ERROR worker request 1 failed\n2040-01-01T00:00:01.000Z INFO worker ready\n"
	if err := os.WriteFile(filepath.Join(s.root, "alpha.log"), []byte(plain), 0600); err != nil {
		t.Fatal(err)
	}
	var zipped bytes.Buffer
	gz := gzip.NewWriter(&zipped)
	if _, err := gz.Write([]byte(plain)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "beta.log.gz"), zipped.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(s.receiver)
	t.Cleanup(collector.Close)
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "OTEL_") && !strings.HasPrefix(kv, "LOG_TRIAGE_TOKENS=") {
			env = append(env, kv)
		}
	}
	if enabled {
		env = append(env, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="+collector.URL+"/v1/traces")
	}
	env = append(env, "OTEL_BSP_SCHEDULE_DELAY=30000", "LOG_TRIAGE_TOKENS=p5-secret-writer=operator,p5-secret-viewer=observer:read-only")
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	args := append(append([]string{}, command...), "--root", s.root, "--reports", s.reports, "--log-level", "debug")
	if transport == "http" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		s.url = "http://" + addr + "/mcp"
		args = append(args, "--http", addr)
	}
	s.cmd = exec.Command(args[0], args[1:]...)
	s.cmd.Env = env
	s.cmd.Stderr = &s.stderr
	if transport == "stdio" {
		var err error
		s.in, err = s.cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := s.cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = s.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() {
			scanner := bufio.NewScanner(out)
			scanner.Buffer(make([]byte, 4096), 4<<20)
			for scanner.Scan() {
				line := append([]byte{}, scanner.Bytes()...)
				s.stdout.Write(append(line, '\n'))
				s.lines <- line
			}
			close(s.lines)
		}()
	} else {
		s.cmd.Stdout = &s.stdout
		if err := s.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	go func() { s.done <- s.cmd.Wait() }()
	t.Cleanup(func() {
		if !s.stopped {
			s.stop("signal")
		}
	})
	if transport == "http" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, err := http.Get(s.url)
			if err == nil {
				resp.Body.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("HTTP did not start: %s", s.stderr.text())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return s
}

func (s *server) rpc(method string, params map[string]any, meta map[string]any, token string) map[string]any {
	s.t.Helper()
	s.next++
	p := map[string]any{}
	for k, v := range params {
		p[k] = v
	}
	m := map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientInfo": map[string]any{"name": "p5-checks", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	for k, v := range meta {
		m[k] = v
	}
	p["_meta"] = m
	msg := map[string]any{"jsonrpc": "2.0", "id": s.next, "method": method, "params": p}
	body, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatal(err)
	}
	var raw []byte
	if s.url == "" {
		if _, err = s.in.Write(append(body, '\n')); err != nil {
			s.t.Fatal(err)
		}
		select {
		case raw = <-s.lines:
		case <-time.After(5 * time.Second):
			s.t.Fatalf("no %s response: %s", method, s.stderr.text())
		}
	} else {
		req, err := http.NewRequest("POST", s.url, bytes.NewReader(body))
		if err != nil {
			s.t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", method)
		if n, ok := p["name"].(string); ok {
			req.Header.Set("Mcp-Name", n)
		}
		if u, ok := p["uri"].(string); ok {
			req.Header.Set("Mcp-Name", u)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			s.t.Fatal(err)
		}
		raw, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			s.t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			s.t.Fatalf("HTTP %d: %s", resp.StatusCode, raw)
		}
		if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "data:") {
					raw = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				}
			}
		}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		s.t.Fatalf("stdout must be JSON-RPC: %q (%v)", raw, err)
	}
	return result
}

func (s *server) call(name string, args map[string]any, meta map[string]any) map[string]any {
	return s.rpc("tools/call", map[string]any{"name": name, "arguments": args}, meta, "p5-secret-writer")
}
func (s *server) stop(how string) []receiver.Record {
	s.t.Helper()
	if s.stopped {
		return s.receiver.Snapshot()
	}
	s.stopped = true
	if how == "eof" && s.in != nil {
		s.in.Close()
	} else {
		if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			s.t.Log(err)
		}
	}
	select {
	case <-s.done:
	case <-time.After(6 * time.Second):
		s.cmd.Process.Kill()
		s.t.Fatal("server did not flush and stop within shutdown bound")
	}
	if s.url != "" && s.stdout.text() != "" {
		s.t.Fatalf("HTTP stdout must be empty: %q", s.stdout.text())
	}
	return s.receiver.Snapshot()
}

func find(t *testing.T, rows []receiver.Record, name string) receiver.Record {
	t.Helper()
	for _, r := range rows {
		if r.Span.Name == name {
			return r
		}
	}
	t.Fatalf("missing %q span: implement this task's instrumentation (%d exported spans)", name, len(rows))
	return receiver.Record{}
}
func attrs(r receiver.Record) map[string]any { return receiver.Attributes(r.Span.Attributes) }
func equal(t *testing.T, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// equalWhy is equal with a sentence that says what is missing.
func equalWhy(t *testing.T, got, want any, why string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: got %v, want %v", why, got, want)
	}
}
func structured(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	r, _ := reply["result"].(map[string]any)
	v, _ := r["structuredContent"].(map[string]any)
	if v == nil {
		t.Fatalf("successful structured result required: %v", reply)
	}
	return v
}
