package checks

// Start the server under test over HTTP (and stdio where a check needs it) and
// talk to it.
//
// Nothing here imports the server. The command comes from LOG_TRIAGE_CMD (a JSON
// array) or is the binary built from <repo>/solution; the checks add --root,
// --reports, --http and any other flags as separate arguments, and set
// LOG_TRIAGE_TOKENS themselves.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	startTimeout = 180 * time.Second // the first build may have to download the server's modules
	callTimeout  = 120 * time.Second
	notFoundCode = jsonrpc.CodeInvalidParams
	modern       = "2026-07-28"
	legacy       = "2025-11-25"
)

var scanIDRE = regexp.MustCompile(`^s_[0-9a-f]{16}$`)

var (
	checksDir string
	repoRoot  string
	s3Logs    string
	serverDir string
	workDir   string // the built server and the temporary reports folders

	expected facts
	calls    = map[string]call{}

	owner, owner2, other, viewer, unknownToken string
)

// facts is checks/expected.json; its "about" field describes the rest.
type facts struct {
	Calls   []call `json:"calls"`
	Tokens  string `json:"tokens"`
	Callers struct {
		Owner    []string `json:"owner"`
		Other    string   `json:"other"`
		ReadOnly string   `json:"read_only"`
		Unknown  string   `json:"unknown"`
	} `json:"callers"`
	Origins struct {
		Allowed   []string `json:"allowed"`
		Forbidden []string `json:"forbidden"`
	} `json:"origins"`
	Prompt struct {
		Name      string `json:"name"`
		Arguments []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"arguments"`
		ToolOrder         []string          `json:"tool_order"`
		Render            map[string]string `json:"render"`
		RenderSymptomOnly map[string]string `json:"render_symptom_only"`
	} `json:"prompt"`
	Tools struct {
		Writer   []string `json:"writer"`
		ReadOnly []string `json:"read_only"`
	} `json:"tools"`
}

type call struct {
	ID      string         `json:"id"`
	Root    string         `json:"root"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Scan    map[string]any `json:"scan"`
	GroupID string         `json:"group_id"`
	Result  any            `json:"result"`
}

const p1Instructions = "Log Triage reads log files under one folder. Start with list_logs to see what is there. " +
	"Use summarize_logs on a file or folder, with since and until when you know the time, to get " +
	"counts and message groups instead of raw lines. Use search_logs to find the lines that match a " +
	"pattern. Record a conclusion with save_finding and cite a file and line for every piece of " +
	"evidence. All times are UTC."

const p3Sentence = "Use timeline with a scan_id to see how often a pattern occurs over time, and how a number in " +
	"those lines moves."

const p4Sentence = "For a guided investigation, use the triage prompt."

const tokensMissing = "LOG_TRIAGE_TOKENS is required with --http"

const e13 = "save_finding is not available to this caller."

func p2Sentence(lifetime string) string {
	return "summarize_logs returns a scan_id that works for " + lifetime + "; pass it to group_lines to read " +
		"one group's lines without scanning again. Use read_lines for the lines around one line. " +
		"Saved findings are listed at triage://reports."
}

func e6(scanID string) string {
	return `scan "` + scanID + `" is unknown or has expired. Call summarize_logs again for a new scan_id.`
}

// ------------------------------------------------------------------ setup

func TestMain(m *testing.M) {
	os.Exit(runChecks(m))
}

func runChecks(m *testing.M) int {
	var err error
	if checksDir, err = os.Getwd(); err != nil {
		fmt.Fprintln(os.Stderr, "checks:", err)
		return 2
	}
	repoRoot = filepath.Dir(filepath.Dir(checksDir))
	s3Logs = filepath.Join(repoRoot, "s3_logs")
	serverDir = filepath.Join(repoRoot, "solution")
	raw, err := os.ReadFile(filepath.Join(checksDir, "expected.json"))
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err == nil && (len(expected.Callers.Owner) != 2 || expected.Tokens == "") {
		err = errors.New("callers.owner must name two tokens, and tokens must be set")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "checks/expected.json:", err)
		return 2
	}
	for _, c := range expected.Calls {
		calls[c.ID] = c
	}
	owner, owner2 = expected.Callers.Owner[0], expected.Callers.Owner[1]
	other, viewer, unknownToken = expected.Callers.Other, expected.Callers.ReadOnly, expected.Callers.Unknown
	temp, err := os.MkdirTemp("", "log-triage-checks-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "checks:", err)
		return 2
	}
	defer os.RemoveAll(temp)
	if workDir, err = filepath.EvalSymlinks(temp); err != nil {
		fmt.Fprintln(os.Stderr, "checks:", err)
		return 2
	}
	code := m.Run()
	closeShared()
	return code
}

// ------------------------------------------------------------------ the command

var (
	commandOnce  sync.Once
	commandLine  []string // the server command, without --root and the other flags
	commandTried []string // what was run when the command couldn't be made
	commandErr   error
)

// serverCommand returns LOG_TRIAGE_CMD, or builds the server into workDir once.
func serverCommand() ([]string, []string, error) {
	commandOnce.Do(func() {
		if raw, ok := os.LookupEnv("LOG_TRIAGE_CMD"); ok && raw != "" {
			var cmd []string
			if err := json.Unmarshal([]byte(raw), &cmd); err != nil {
				commandErr = fmt.Errorf(`LOG_TRIAGE_CMD must be a JSON array of strings, like ["./bin/log-triage"]: %v`, err)
			} else if len(cmd) == 0 {
				commandErr = errors.New("LOG_TRIAGE_CMD must be a non-empty JSON array of strings")
			} else {
				commandLine = cmd
			}
			commandTried = []string{"LOG_TRIAGE_CMD=" + raw}
			return
		}
		bin := filepath.Join(workDir, "log-triage")
		commandTried = []string{"go", "build", "-C", serverDir, "-o", bin, "./cmd/log-triage"}
		if _, err := os.Stat(filepath.Join(serverDir, "go.mod")); err != nil {
			commandErr = errors.New("solution/go.mod does not exist, so there is no server module to build")
			return
		}
		build := exec.Command(commandTried[0], commandTried[1:]...)
		build.Env = append(os.Environ(), "GOWORK=off")
		if out, err := build.CombinedOutput(); err != nil {
			commandErr = fmt.Errorf("the server does not build (%v):\n%s", err, strings.TrimSpace(string(out)))
			return
		}
		commandLine = []string{bin}
	})
	return commandLine, commandTried, commandErr
}

var shellSafe = regexp.MustCompile(`^[\w@%+=:,./-]+$`)

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if shellSafe.MatchString(arg) {
			quoted[i] = arg
		} else {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

// childEnv is the checks' environment with LOG_TRIAGE_TOKENS set to tokens, or
// removed when tokens is nil.
func childEnv(tokens *string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LOG_TRIAGE_TOKENS=") {
			env = append(env, kv)
		}
	}
	if tokens != nil {
		env = append(env, "LOG_TRIAGE_TOKENS="+*tokens)
	}
	return env
}

// fill replaces {key} in every string of value.
func fill(value any, places map[string]string) any {
	switch v := value.(type) {
	case string:
		for _, key := range sortedKeys(places) {
			v = strings.ReplaceAll(v, "{"+key+"}", places[key])
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = fill(item, places)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = fill(item, places)
		}
		return out
	}
	return value
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

var dateRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// corpusNames lists the host folder names under s3_logs and the dates in its
// file names.
func corpusNames(t *testing.T) []string {
	t.Helper()
	names := map[string]bool{}
	groups, err := os.ReadDir(s3Logs)
	if err != nil {
		t.Fatalf("reading s3_logs: %v", err)
	}
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		hosts, err := os.ReadDir(filepath.Join(s3Logs, group.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range hosts {
			if host.IsDir() {
				names[host.Name()] = true
			}
		}
	}
	err = filepath.WalkDir(s3Logs, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, date := range dateRE.FindAllString(d.Name(), -1) {
			names[date] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sortedKeys(names)
}

// ------------------------------------------------------------------ replies

type reply struct {
	tool        string
	arguments   map[string]any
	wireErr     *jsonrpc.Error
	isError     bool
	structured  any
	texts       []string
	otherBlocks int
}

func (r *reply) label() string {
	return fmt.Sprintf("%s(%s)", r.tool, cut(jsonText(r.arguments), 160))
}

func (r *reply) summary() string {
	if r.wireErr != nil {
		return fmt.Sprintf("JSON-RPC error %d: %s", r.wireErr.Code, cut(r.wireErr.Message, 300))
	}
	parts := make([]string, len(r.texts))
	for i, text := range r.texts {
		parts[i] = cut(text, 300)
	}
	return fmt.Sprintf("isError=%v, text: %s", r.isError, strings.Join(parts, " | "))
}

// ok checks a successful call: no error, structuredContent, and one text block
// with the same JSON.
func ok(t *testing.T, r *reply) map[string]any {
	t.Helper()
	where := r.label()
	if r.wireErr != nil {
		t.Fatalf("%s failed: %s", where, r.summary())
	}
	if r.isError {
		t.Fatalf("%s returned a tool error: %s", where, r.summary())
	}
	result, isObject := r.structured.(map[string]any)
	if !isObject {
		t.Fatalf("%s returned no structuredContent", where)
	}
	if len(r.texts) != 1 || r.otherBlocks != 0 {
		t.Fatalf("%s returned %d text blocks and %d other blocks; expected one text block holding the "+
			"same object as JSON", where, len(r.texts), r.otherBlocks)
	}
	var parsed any
	if err := json.Unmarshal([]byte(r.texts[0]), &parsed); err != nil {
		t.Fatalf("%s: the text block is not JSON: %q", where, cut(r.texts[0], 200))
	}
	if !reflect.DeepEqual(parsed, r.structured) {
		t.Fatalf("%s: the text block's JSON differs from structuredContent", where)
	}
	return result
}

// toolError checks a call refused by the tool itself with exactly the contract's text.
func toolError(t *testing.T, r *reply, text string) {
	t.Helper()
	if r.wireErr != nil {
		t.Fatalf("%s must come back as a tool error (a result with isError: true), got %s", r.label(), r.summary())
	}
	if !r.isError {
		t.Fatalf("%s was accepted; it must answer: %s", r.label(), text)
	}
	if r.structured != nil {
		t.Fatalf("%s is an error but carries structuredContent", r.label())
	}
	if got := strings.Join(r.texts, " "); got != text {
		t.Fatalf("%s: the error text must be exactly the contract's (CONTRACT.md, Errors).\nexpected: %q\ngot:      %q",
			r.label(), text, got)
	}
}

// same compares values; an object may hold keys the facts don't name.
func same(actual, want any, where string) error {
	switch w := want.(type) {
	case map[string]any:
		a, isObject := actual.(map[string]any)
		if !isObject {
			return fmt.Errorf("%s: expected an object, got %s", where, cut(jsonText(actual), 200))
		}
		var missing []string
		for _, key := range sortedKeys(w) {
			if _, found := a[key]; !found {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("%s: missing field(s) %v", where, missing)
		}
		for _, key := range sortedKeys(w) {
			if err := same(a[key], w[key], where+"."+key); err != nil {
				return err
			}
		}
	case []any:
		a, isList := actual.([]any)
		if !isList {
			return fmt.Errorf("%s: expected a list, got %s", where, cut(jsonText(actual), 200))
		}
		for i := 0; i < len(a) && i < len(w); i++ {
			if err := same(a[i], w[i], fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
		if len(a) != len(w) {
			return fmt.Errorf("%s: expected %d items, got %d", where, len(w), len(a))
		}
	default:
		if reflect.TypeOf(actual) != reflect.TypeOf(want) || actual != want {
			return fmt.Errorf("%s: expected %s, got %s", where, cut(jsonText(want), 200), cut(jsonText(actual), 200))
		}
	}
	return nil
}

// rawReply is one HTTP exchange: status, headers and the JSON-RPC message, if any.
type rawReply struct {
	label   string
	status  int
	headers http.Header
	body    string
	message map[string]any
}

func (r *rawReply) describe() string {
	return fmt.Sprintf("%s: HTTP %d, body %q", r.label, r.status, cut(r.body, 300))
}

func (r *rawReply) result(t *testing.T) map[string]any {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("%s; expected HTTP 200", r.describe())
	}
	if r.message == nil {
		t.Fatalf("%s; expected one JSON-RPC answer", r.describe())
	}
	if _, isError := r.message["error"]; isError {
		t.Fatalf("%s; expected a result", r.describe())
	}
	result, _ := r.message["result"].(map[string]any)
	return result
}

// parseMessage finds the JSON-RPC answer in a JSON body or in the last data line
// of an event stream.
func parseMessage(contentType, body string) map[string]any {
	text := strings.TrimSpace(body)
	if strings.Contains(contentType, "text/event-stream") {
		text = ""
		for _, line := range strings.Split(body, "\n") {
			if data, found := strings.CutPrefix(line, "data:"); found {
				text = strings.TrimSpace(data)
			}
		}
	}
	var message map[string]any
	if json.Unmarshal([]byte(text), &message) != nil || message["jsonrpc"] != "2.0" {
		return nil
	}
	return message
}

// refused checks an SDK refusal by its JSON-RPC answer, never by the HTTP status.
func refused(t *testing.T, r *rawReply, why string) {
	t.Helper()
	if r.message == nil {
		t.Fatalf("%s; %s, and the answer must still be JSON-RPC", r.describe(), why)
	}
	if _, isError := r.message["error"]; isError {
		return
	}
	result, _ := r.message["result"].(map[string]any)
	if result["isError"] != true || result["structuredContent"] != nil {
		t.Fatalf("%s; %s, so it must be refused (a JSON-RPC error, or isError without structuredContent)", r.describe(), why)
	}
}

// rpcError checks for a JSON-RPC error answer, whatever its code and HTTP
// status: a result, even one marked as an error, fails.
func rpcError(t *testing.T, r *rawReply, why string) {
	t.Helper()
	if r.message == nil {
		t.Fatalf("%s; %s, and the answer must still be JSON-RPC", r.describe(), why)
	}
	failure, isObject := r.message["error"].(map[string]any)
	code, isNumber := failure["code"].(float64)
	if !isObject || !isNumber || code != float64(int64(code)) {
		t.Fatalf("%s; %s, so the answer must be a JSON-RPC error with an integer code (CONTRACT.md, section 5), not a result",
			r.describe(), why)
	}
}

func bearerChallenge(t *testing.T, r *rawReply) {
	t.Helper()
	if r.status != http.StatusUnauthorized {
		t.Fatalf("%s; a request without a known token must answer 401. Task 1.", r.describe())
	}
	challenge := r.headers.Get("WWW-Authenticate")
	scheme, _, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "bearer") {
		t.Fatalf("%s: a 401 must carry a WWW-Authenticate header whose scheme is Bearer, got %q", r.label, challenge)
	}
}

// ------------------------------------------------------------------ the server over HTTP

// httpServer is one server process with --http on a free local port.
type httpServer struct {
	root    string
	reports string
	port    int
	url     string
	command []string
	tokens  *string
	task    string

	once    sync.Once
	proc    *exec.Cmd
	exited  chan struct{}
	stdout  syncBuffer
	stderr  syncBuffer
	failure string
}

func newHTTPServer(reports string, tokens *string, flags ...string) *httpServer {
	root, err := filepath.EvalSymlinks(s3Logs)
	if err != nil {
		root = s3Logs
	}
	s := &httpServer{root: root, reports: reports, tokens: tokens, task: "Task 1"}
	args := []string{"--root", s3Logs}
	if reports != "" {
		args = append(args, "--reports", reports)
	}
	s.command = append(args, flags...)
	return s
}

func withTokens() *string {
	tokens := expected.Tokens
	return &tokens
}

func (s *httpServer) start() {
	s.once.Do(func() {
		base, tried, err := serverCommand()
		if err != nil {
			s.command, s.failure = tried, err.Error()
			return
		}
		if s.port, err = freePort(); err != nil {
			s.failure = err.Error()
			return
		}
		addr := fmt.Sprintf("127.0.0.1:%d", s.port)
		s.url = "http://" + addr + "/mcp"
		s.command = append(append(append([]string{}, base...), s.command...), "--http", addr)
		s.proc = exec.Command(s.command[0], s.command[1:]...)
		s.proc.Dir = checksDir
		s.proc.Env = childEnv(s.tokens)
		s.proc.Stdout = &s.stdout
		s.proc.Stderr = &s.stderr
		if err := s.proc.Start(); err != nil {
			s.failure = err.Error()
			return
		}
		s.exited = make(chan struct{})
		go func() {
			_ = s.proc.Wait()
			close(s.exited)
		}()
		deadline := time.Now().Add(startTimeout)
		for time.Now().Before(deadline) {
			select {
			case <-s.exited:
				s.failure = fmt.Sprintf("the server exited with code %d", s.proc.ProcessState.ExitCode())
				return
			default:
			}
			if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
				conn.Close()
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		s.failure = fmt.Sprintf("nothing listened on %s within %s", addr, startTimeout)
	})
}

// exitCode is, for a server that should refuse to start, its exit code, or -1
// if it kept running.
func (s *httpServer) exitCode() int {
	s.start()
	if s.exited == nil {
		return -1
	}
	select {
	case <-s.exited:
		return s.proc.ProcessState.ExitCode()
	default:
		return -1
	}
}

func (s *httpServer) close() {
	if s.proc == nil || s.exited == nil {
		return
	}
	select {
	case <-s.exited:
		return
	default:
	}
	_ = s.proc.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.exited:
	case <-time.After(10 * time.Second):
		_ = s.proc.Process.Kill()
		<-s.exited
	}
}

func (s *httpServer) require(t *testing.T) {
	t.Helper()
	s.start()
	if s.failure != "" {
		t.Fatalf("The server did not start over HTTP, so this check can't run.\n"+
			"Command: %s\nError: %s\nLast lines on the server's stderr:\n%s\n"+
			"%s of the practice README adds --http; the server must start with these flags and "+
			"LOG_TRIAGE_TOKENS set. To test another build, set LOG_TRIAGE_CMD to its command as a JSON array.",
			shellJoin(s.command), s.failure, tail(s.stderr.String(), 15), s.task)
	}
}

// post sends one JSON-RPC body; token "" sends no Authorization header.
func (s *httpServer) post(t *testing.T, body map[string]any, token, origin string, headers map[string]string, label string) *rawReply {
	t.Helper()
	s.require(t)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := (&http.Client{Timeout: callTimeout}).Do(req)
	if err != nil {
		t.Fatalf("%s got no HTTP answer: %v\nLast lines on the server's stderr:\n%s", label, err, tail(s.stderr.String(), 15))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if label == "" {
		label, _ = body["method"].(string)
	}
	return &rawReply{label: label, status: resp.StatusCode, headers: resp.Header, body: string(raw),
		message: parseMessage(resp.Header.Get("Content-Type"), string(raw))}
}

var namedMethods = map[string]bool{"tools/call": true, "prompts/get": true, "resources/read": true}

// rpc sends one request in protocol revision 2026-07-28, with the headers it
// needs on HTTP.
func (s *httpServer) rpc(t *testing.T, method string, params map[string]any, token, origin string, headers map[string]string) *rawReply {
	t.Helper()
	sent := map[string]string{"MCP-Protocol-Version": modern, "Mcp-Method": method}
	withMeta := map[string]any{}
	for key, value := range params {
		withMeta[key] = value
	}
	if namedMethods[method] {
		name, _ := params["name"].(string)
		if name == "" {
			name, _ = params["uri"].(string)
		}
		sent["Mcp-Name"] = name
	}
	for key, value := range headers {
		sent[key] = value
	}
	withMeta["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    modern,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "log-triage-checks", "version": "4"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": withMeta}
	label := fmt.Sprintf("%s %s (token %q, Origin %q)", method, cut(jsonText(params), 120), token, origin)
	return s.post(t, body, token, origin, sent, label)
}

// headerTransport adds a bearer token to every request.
type headerTransport struct {
	token string
	base  http.RoundTripper
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.token != "" {
		r.Header.Set("Authorization", "Bearer "+h.token)
	}
	return h.base.RoundTrip(r)
}

// session opens a go-sdk client connection with token; the caller closes it.
func (s *httpServer) session(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	s.require(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "log-triage-checks", Version: "4"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   s.url,
		HTTPClient: &http.Client{Transport: headerTransport{token, http.DefaultTransport}, Timeout: callTimeout},
		MaxRetries: -1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connecting over HTTP with token %q failed: %v\nLast lines on the server's stderr:\n%s",
			token, err, tail(s.stderr.String(), 15))
	}
	return session
}

func (s *httpServer) toolNames(t *testing.T, token string) []string {
	t.Helper()
	session := s.session(t, token)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list over HTTP with token %q failed: %v", token, err)
		}
		names = append(names, tool.Name)
	}
	return names
}

func (s *httpServer) call(t *testing.T, name string, arguments map[string]any, token string) *reply {
	t.Helper()
	session := s.session(t, token)
	defer session.Close()
	if arguments == nil {
		arguments = map[string]any{}
	}
	r := &reply{tool: name, arguments: arguments}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		var wire *jsonrpc.Error
		if errors.As(err, &wire) {
			r.wireErr = wire
			return r
		}
		t.Fatalf("%s got no MCP answer: %v\nLast lines on the server's stderr:\n%s", r.label(), err, tail(s.stderr.String(), 15))
	}
	r.isError = result.IsError
	r.structured = result.StructuredContent
	for _, block := range result.Content {
		if text, isText := block.(*mcp.TextContent); isText {
			r.texts = append(r.texts, text.Text)
		} else {
			r.otherBlocks++
		}
	}
	return r
}

func (s *httpServer) scan(t *testing.T, arguments map[string]any, token string) string {
	t.Helper()
	result := ok(t, s.call(t, "summarize_logs", arguments, token))
	scanID, _ := result["scan_id"].(string)
	if !scanIDRE.MatchString(scanID) {
		t.Fatalf("summarize_logs must return a scan_id like s_ and 16 lowercase hex characters, got %q", scanID)
	}
	return scanID
}

// read returns a resource's contents, or the JSON-RPC error that refused it.
func (s *httpServer) read(t *testing.T, uri, token string) (*jsonrpc.Error, []*mcp.ResourceContents) {
	t.Helper()
	session := s.session(t, token)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		var wire *jsonrpc.Error
		if errors.As(err, &wire) {
			return wire, nil
		}
		t.Fatalf("reading %s got no MCP answer: %v", uri, err)
	}
	return nil, result.Contents
}

func (s *httpServer) prompts(t *testing.T, token string) []*mcp.Prompt {
	t.Helper()
	session := s.session(t, token)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("prompts/list over HTTP failed: %v", err)
	}
	return result.Prompts
}

func (s *httpServer) getPrompt(t *testing.T, arguments map[string]string, token string) *mcp.GetPromptResult {
	t.Helper()
	session := s.session(t, token)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: expected.Prompt.Name, Arguments: arguments})
	if err != nil {
		t.Fatalf("prompts/get %s with %v failed: %v", expected.Prompt.Name, arguments, err)
	}
	return result
}

// runCall makes one expected.json call: its scan made with scanToken, the call
// made with callToken.
func runCall(t *testing.T, s *httpServer, callID, scanToken, callToken string) (*reply, map[string]string) {
	t.Helper()
	c, found := calls[callID]
	if !found {
		t.Fatalf("expected.json has no call %q", callID)
	}
	places := map[string]string{"root": s.root}
	args := map[string]any{}
	for key, value := range c.Args {
		args[key] = value
	}
	if c.Scan != nil {
		places["scan_id"] = s.scan(t, c.Scan, scanToken)
		args["scan_id"] = places["scan_id"]
		if c.GroupID != "" {
			args["group_id"] = c.GroupID
		}
	}
	return s.call(t, c.Tool, args, callToken), places
}

func checkAnswer(t *testing.T, s *httpServer, callID, scanToken, callToken string) map[string]any {
	t.Helper()
	c := calls[callID]
	r, places := runCall(t, s, callID, scanToken, callToken)
	result := ok(t, r)
	if err := same(jsonValue(t, result), fill(c.Result, places), c.Tool+" "+callID); err != nil {
		t.Fatal(err)
	}
	return result
}

// ------------------------------------------------------------------ shared servers

var (
	sharedMu      sync.Mutex
	sharedServers = map[string]*httpServer{}
)

// module holds the server of one check file, like a module-scoped fixture: the
// server on s3_logs with the checks' tokens, writing findings to a temporary
// folder. It starts on first use.
type module string

func (m module) server(t *testing.T) *httpServer {
	t.Helper()
	sharedMu.Lock()
	s, found := sharedServers[string(m)]
	if !found {
		reports := filepath.Join(workDir, "reports-"+string(m))
		s = newHTTPServer(reports, withTokens())
		sharedServers[string(m)] = s
	}
	sharedMu.Unlock()
	s.require(t)
	return s
}

func closeShared() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	for _, s := range sharedServers {
		s.close()
	}
}

// ownHTTPServer starts a server for one test and stops it when the test ends.
func ownHTTPServer(t *testing.T, tokens *string, flags ...string) *httpServer {
	t.Helper()
	s := newHTTPServer(t.TempDir(), tokens, flags...)
	t.Cleanup(s.close)
	return s
}

// ------------------------------------------------------------------ the server over stdio

// connectStdio starts the server over stdio with --root s3_logs and flags. It
// returns the session, or why it didn't start.
func connectStdio(flags ...string) (*mcp.ClientSession, string) {
	base, tried, err := serverCommand()
	if err != nil {
		return nil, fmt.Sprintf("The server did not start over stdio: %v\nCommand: %s", err, shellJoin(tried))
	}
	command := append(append(append([]string{}, base...), "--root", s3Logs), flags...)
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = checksDir
	cmd.Env = childEnv(nil)
	var stderr syncBuffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "log-triage-checks", Version: "4"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: 5 * time.Second}, nil)
	if err != nil {
		return nil, fmt.Sprintf("The server did not start over stdio: %v\nCommand: %s\nLast lines on its stderr:\n%s",
			err, shellJoin(command), tail(stderr.String(), 15))
	}
	return session, ""
}

// ------------------------------------------------------------------ small helpers

// syncBuffer collects a process's output while tests read it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func tail(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if joined := strings.Join(lines, "\n"); joined != "" {
		return joined
	}
	return "(nothing)"
}

// jsonValue returns v as encoding/json decodes it into an any.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonText(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
