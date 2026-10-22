package checks

// Start the server under test and talk to it over MCP.
//
// Nothing here imports the server. The command comes from LOG_TRIAGE_CMD (a JSON
// array) or is the binary built from <repo>/solution; the checks add --root,
// --reports and any other flags as separate arguments.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	startTimeout = 180 * time.Second // the first build may have to download the server's modules
	callTimeout  = 120 * time.Second
	notFoundCode = jsonrpc.CodeInvalidParams
)

var (
	timeRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	scanIDRE = regexp.MustCompile(`^s_[0-9a-f]{16}$`)
)

var (
	checksDir string
	repoRoot  string
	s3Logs    string
	serverDir string
	workDir   string // the built server, the temporary log roots and reports folders

	expected facts
	calls    = map[string]call{}
)

// facts is checks/expected.json; its "about" field describes the rest.
type facts struct {
	Trees           map[string]treeSpec `json:"trees"`
	Calls           []call              `json:"calls"`
	FormatsDocument any                 `json:"formats_document"`
}

type treeSpec struct {
	Root    string      `json:"root"`
	Entries []treeEntry `json:"entries"`
}

type treeEntry struct {
	Path     string    `json:"path"`
	Symlink  *string   `json:"symlink"`
	Copy     string    `json:"copy"`
	Gunzip   string    `json:"gunzip"`
	Bytes    *string   `json:"bytes"`
	Text     []string  `json:"text"`
	Fill     *fillSpec `json:"fill"`
	Repeat   int       `json:"repeat"`
	Numbered bool      `json:"numbered"`
	Gzip     bool      `json:"gzip"`
	CutHalf  bool      `json:"cut_half"`
	Size     *int64    `json:"size"`
}

type fillSpec struct {
	Char  string `json:"char"`
	Count int    `json:"count"`
}

type call struct {
	ID      string         `json:"id"`
	Root    string         `json:"root"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Scan    map[string]any `json:"scan"`
	GroupID string         `json:"group_id"`
	Result  any            `json:"result"`
	Error   *string        `json:"error"`
	Compare string         `json:"compare"`
}

const p1Instructions = "Log Triage reads log files under one folder. Start with list_logs to see what is there. " +
	"Use summarize_logs on a file or folder, with since and until when you know the time, to get " +
	"counts and message groups instead of raw lines. Use search_logs to find the lines that match a " +
	"pattern. Record a conclusion with save_finding and cite a file and line for every piece of " +
	"evidence. All times are UTC."

func p2Sentence(lifetime string) string {
	return "summarize_logs returns a scan_id that works for " + lifetime + "; pass it to group_lines to read " +
		"one group's lines without scanning again. Use read_lines for the lines around one line. " +
		"Saved findings are listed at triage://reports."
}

// ------------------------------------------------------------------ contract texts (CONTRACT.md, Errors)

func e1(path string) string {
	return `path "` + path + `" is outside the log root. Pass a path relative to the log root; ` +
		`list_logs with path "." shows what is there.`
}

func e2(path string) string {
	return `path "` + path + `" does not exist under the log root. Call list_logs to see what is there.`
}

func e3(field string) string {
	return field + " must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z."
}

const e4 = "until must be later than since."

func e5(field string) string {
	return field + " is not a valid regular expression. Escape special characters such as ( [ . * " +
		"with a backslash, or search for a plain word."
}

func e6(scanID string) string {
	return `scan "` + scanID + `" is unknown or has expired. Call summarize_logs again for a new scan_id.`
}

func e8(line int, file string, total int) string {
	return fmt.Sprintf(`line %d is past the end of "%s", which has %d lines.`, line, file, total)
}

func e9(name string) string {
	return `a finding named "` + name + `" already exists. Choose a different title.`
}

func e10(path string) string {
	return `"` + path + `" is a folder. This needs one file; call list_logs to pick one.`
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
	if err != nil {
		fmt.Fprintln(os.Stderr, "checks/expected.json:", err)
		return 2
	}
	for _, c := range expected.Calls {
		calls[c.ID] = c
	}
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

func startFailure(command []string, failure, stderr, task string) string {
	return "The server did not start, so this check can't run.\n" +
		"Command: " + shellJoin(command) + "\n" +
		"Error: " + failure + "\n" +
		"Last lines on the server's stderr:\n" + stderr + "\n" +
		task + " (see the practice README) must leave a server that starts with these flags. " +
		"To test another build, set LOG_TRIAGE_CMD to its command as a JSON array."
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

// callsFor lists the ids of the expected calls of one tool that have kind
// ("result" or "error"), in file order.
func callsFor(tool, kind string) []string {
	var ids []string
	for _, c := range expected.Calls {
		has := c.Result != nil
		if kind == "error" {
			has = c.Error != nil
		}
		if c.Tool == tool && has {
			ids = append(ids, c.ID)
		}
	}
	return ids
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

// ------------------------------------------------------------------ trees

// makeEntry writes one file or link of a temporary tree; expected.json's
// "about" lists the kinds.
func makeEntry(entry treeEntry, base string) error {
	target := filepath.Join(base, filepath.FromSlash(entry.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	switch {
	case entry.Symlink != nil:
		return os.Symlink(*entry.Symlink, target)
	case entry.Copy != "":
		data, err := os.ReadFile(filepath.Join(s3Logs, filepath.FromSlash(entry.Copy)))
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	case entry.Gunzip != "":
		data, err := gunzipFile(filepath.Join(s3Logs, filepath.FromSlash(entry.Gunzip)))
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	case entry.Bytes != nil:
		return os.WriteFile(target, []byte(*entry.Bytes), 0o644)
	}
	lines := append([]string{}, entry.Text...)
	if entry.Fill != nil {
		filler := strings.Repeat(entry.Fill.Char, entry.Fill.Count)
		for i, line := range lines {
			lines[i] = strings.ReplaceAll(line, "{fill}", filler)
		}
	}
	block := strings.Join(lines, "\n") + "\n"
	repeat := max(entry.Repeat, 1)
	var buf bytes.Buffer
	var out io.Writer = &buf
	var zw *gzip.Writer
	if entry.Gzip {
		var err error
		if zw, err = gzip.NewWriterLevel(&buf, 6); err != nil {
			return err
		}
		out = zw
	}
	bw := bufio.NewWriterSize(out, 1<<16)
	for i := 1; i <= repeat; i++ {
		text := block
		if entry.Numbered {
			text = strings.ReplaceAll(block, "{n}", fmt.Sprint(i))
		}
		if _, err := bw.WriteString(text); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			return err
		}
	}
	data := buf.Bytes()
	if entry.Gzip && entry.CutHalf {
		data = data[:len(data)/2]
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return err
	}
	if entry.Size != nil {
		return os.Truncate(target, *entry.Size)
	}
	return nil
}

// tree is a temporary log tree: base holds the entries, root is the log root
// inside it.
type tree struct {
	base string
	root string
}

func buildTree(name, dest string) (tree, error) {
	spec, found := expected.Trees[name]
	if !found {
		return tree{}, fmt.Errorf("expected.json has no tree %q", name)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return tree{}, err
	}
	base, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return tree{}, err
	}
	for _, entry := range spec.Entries {
		if err := makeEntry(entry, base); err != nil {
			return tree{}, fmt.Errorf("building %s: %v", entry.Path, err)
		}
	}
	return tree{base: base, root: filepath.Join(base, filepath.FromSlash(spec.Root))}, nil
}

func gunzipFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// tempDir is a new folder for one test, with symlinks in its path resolved.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
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
		t.Fatalf("%s returned no structuredContent. Give the tool an output type the SDK turns into "+
			"an output schema (see CONTRACT.md, Tools).", where)
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
	dec := json.NewDecoder(strings.NewReader(r.texts[0]))
	dec.UseNumber()
	var exact any
	if err := dec.Decode(&exact); err == nil {
		if field := fractionalInteger(exact, ""); field != "" {
			t.Fatalf("%s: %s is a whole number and must be written without a fraction or exponent", where, field)
		}
	}
	return result
}

// integerFields are the output fields that hold whole numbers.
var integerFields = map[string]bool{
	"total": true, "shown": true, "count": true, "size_bytes": true, "line": true, "files_scanned": true,
	"lines_scanned": true, "lines_in_window": true, "total_groups": true, "file_count": true,
	"critical": true, "error": true, "warning": true, "info": true, "debug": true, "unknown": true,
	"start": true, "end": true, "total_lines": true,
}

// fractionalInteger returns the path of the first whole-number field written with
// a fraction or an exponent, or "". The SDK decodes structuredContent into
// float64, so only the text copy shows how a number was written.
func fractionalInteger(value any, where string) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(v) {
			if n, isNumber := v[key].(json.Number); isNumber && integerFields[key] && strings.ContainsAny(n.String(), ".eE") {
				return where + "." + key
			}
			if found := fractionalInteger(v[key], where+"."+key); found != "" {
				return found
			}
		}
	case []any:
		for i, item := range v {
			if found := fractionalInteger(item, fmt.Sprintf("%s[%d]", where, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

// refusedBySchema checks a call refused before the tool runs: a JSON-RPC error,
// or isError with no structuredContent.
func refusedBySchema(t *testing.T, r *reply, why string) {
	t.Helper()
	if r.wireErr != nil {
		return
	}
	if !r.isError {
		t.Fatalf("%s was accepted, but %s; it must be refused before the tool runs", r.label(), why)
	}
	if r.structured != nil {
		t.Fatalf("%s is an error but carries structuredContent", r.label())
	}
}

// toolError checks a call refused by the tool itself with exactly the contract's
// text. When root is not in the arguments, the text must not show it either.
func toolError(t *testing.T, r *reply, text, root string) {
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
	got := strings.Join(r.texts, " ")
	if got != text {
		t.Fatalf("%s: the error text must be exactly the contract's (CONTRACT.md, Errors).\nexpected: %q\ngot:      %q",
			r.label(), text, got)
	}
	if root != "" && !strings.Contains(jsonText(r.arguments), root) && strings.Contains(got, root) {
		t.Fatalf("%s: the error text shows the log root's absolute path", r.label())
	}
}

// same compares values; an object may hold keys the facts don't name (later
// practices add some).
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

func validate(schema any, value any) error {
	if schema == nil {
		return nil
	}
	var s jsonschema.Schema
	if err := remarshal(schema, &s); err != nil {
		return fmt.Errorf("the outputSchema is not a JSON schema: %v", err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return fmt.Errorf("the outputSchema does not resolve: %v", err)
	}
	return resolved.Validate(value)
}

// runCall makes one expected.json call; a group_lines call scans first. It
// returns the reply and the places its texts are filled with.
func runCall(t *testing.T, s *server, callID string) (*reply, map[string]string) {
	t.Helper()
	c := calls[callID]
	places := map[string]string{"root": s.root}
	var args map[string]any
	if c.Tool == "group_lines" {
		places["scan_id"] = s.scan(t, fill(c.Scan, places).(map[string]any))
		args = map[string]any{"scan_id": places["scan_id"], "group_id": c.GroupID}
		for key, value := range c.Args {
			args[key] = value
		}
	} else {
		args = fill(c.Args, places).(map[string]any)
	}
	return s.call(t, c.Tool, args), places
}

func checkAnswer(t *testing.T, s *server, callID, hint string) map[string]any {
	t.Helper()
	c := calls[callID]
	r, places := runCall(t, s, callID)
	where := c.Tool + " " + callID
	result := ok(t, r)
	if err := validate(s.tool(t, c.Tool, "")["outputSchema"], result); err != nil {
		t.Fatalf("%s: structuredContent does not match the outputSchema: %v", where, err)
	}
	want := fill(c.Result, places)
	var err error
	if c.Compare == "path and format" {
		err = samePathsAndFormats(result, want.(map[string]any), where)
	} else {
		err = same(result, want, where)
	}
	if err != nil {
		if hint != "" {
			t.Fatalf("%v\n%s", err, hint)
		}
		t.Fatal(err)
	}
	return result
}

// samePathsAndFormats compares only path and format of each file, and the rest
// of the result as same does.
func samePathsAndFormats(result, want map[string]any, where string) error {
	pairs := func(v any) [][2]any {
		var out [][2]any
		files, _ := v.([]any)
		for _, f := range files {
			file, _ := f.(map[string]any)
			out = append(out, [2]any{file["path"], file["format"]})
		}
		return out
	}
	if got := pairs(result["files"]); !reflect.DeepEqual(got, pairs(want["files"])) {
		return fmt.Errorf("%s: files differ: %v", where, got)
	}
	rest := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for key, value := range m {
			if key != "files" {
				out[key] = value
			}
		}
		return out
	}
	return same(rest(result), rest(want), where)
}

func checkError(t *testing.T, s *server, callID string) {
	t.Helper()
	r, places := runCall(t, s, callID)
	toolError(t, r, fill(*calls[callID].Error, places).(string), s.root)
}

// notFound checks the protocol's resource-not-found error, with the URI in its
// message or in data.uri.
func notFound(t *testing.T, r *resourceReply, uri string) {
	t.Helper()
	if r.wireErr == nil {
		t.Fatalf("reading %s must fail with the resource-not-found error (JSON-RPC %d), got a result: %s",
			uri, notFoundCode, cut(jsonText(r.contents), 200))
	}
	if r.wireErr.Code != notFoundCode {
		t.Fatalf("reading %s: the error code must be %d, got %d (%s)", uri, notFoundCode, r.wireErr.Code, cut(r.wireErr.Message, 200))
	}
	var data struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(r.wireErr.Data, &data)
	if !strings.Contains(r.wireErr.Message, uri) && data.URI != uri {
		t.Fatalf("reading %s: the error must carry the URI in its message or in data.uri, got message %q and data %s",
			uri, r.wireErr.Message, string(r.wireErr.Data))
	}
}

// masked checks an unexpected error: a tool error that shows nothing of what
// went wrong inside.
func masked(t *testing.T, r *reply, root string) {
	t.Helper()
	if r.wireErr != nil || !r.isError {
		t.Fatalf("%s must come back as a tool error, got %s", r.label(), r.summary())
	}
	if r.structured != nil {
		t.Fatalf("%s is an error but carries structuredContent", r.label())
	}
	text := strings.Join(r.texts, " ")
	for _, leak := range []string{root, "Errno", "Traceback", "Not a gzipped file", "BadGzipFile", "gzip: invalid header"} {
		if strings.Contains(text, leak) {
			t.Fatalf("%s: an unexpected error must be masked (CONTRACT.md, Errors), but the text shows %q: %q",
				r.label(), leak, cut(text, 300))
		}
	}
}

// ------------------------------------------------------------------ schemas

type object = map[string]any

func resolve(t *testing.T, schema any, root object) object {
	t.Helper()
	node, _ := schema.(object)
	for node != nil {
		ref, isRef := node["$ref"].(string)
		if !isRef {
			break
		}
		if !strings.HasPrefix(ref, "#/") {
			t.Fatalf("unsupported $ref %s", ref)
		}
		var at any = root
		for _, part := range strings.Split(ref[2:], "/") {
			m, _ := at.(object)
			at = m[part]
		}
		node, _ = at.(object)
	}
	return node
}

func branches(t *testing.T, schema any, root object) []object {
	t.Helper()
	node := resolve(t, schema, root)
	for _, key := range []string{"anyOf", "oneOf"} {
		if list, found := node[key].([]any); found {
			var out []object
			for _, branch := range list {
				out = append(out, branches(t, branch, root)...)
			}
			return out
		}
	}
	return []object{node}
}

func typesOf(t *testing.T, schema any, root object) []string {
	t.Helper()
	found := map[string]bool{}
	for _, branch := range branches(t, schema, root) {
		switch kind := branch["type"].(type) {
		case string:
			found[kind] = true
		case []any:
			for _, k := range kind {
				if s, isString := k.(string); isString {
					found[s] = true
				}
			}
		}
	}
	return sortedKeys(found)
}

// valueBranch is the branch that isn't null, for bounds on a nullable parameter.
func valueBranch(t *testing.T, schema any, root object) object {
	t.Helper()
	var real []object
	for _, branch := range branches(t, schema, root) {
		kind := branch["type"]
		if kind == "null" || reflect.DeepEqual(kind, []any{"null"}) {
			continue
		}
		real = append(real, branch)
	}
	if len(real) != 1 {
		t.Fatalf("expected one non-null branch in %s", jsonText(schema))
	}
	return real[0]
}

func inputProperty(t *testing.T, tool object, name string) any {
	t.Helper()
	schema, _ := tool["inputSchema"].(object)
	props, _ := schema["properties"].(object)
	prop, found := props[name]
	if !found {
		t.Fatalf("%s: the input schema has no parameter %q", tool["name"], name)
	}
	return prop
}

type bound struct {
	key  string
	want any
}

func checkPlain(t *testing.T, tool object, name, kind string, bounds ...bound) {
	t.Helper()
	root, _ := tool["inputSchema"].(object)
	prop := inputProperty(t, tool, name)
	kinds := typesOf(t, prop, root)
	if !reflect.DeepEqual(kinds, []string{kind}) {
		t.Fatalf("%s.%s: type must be exactly %q, got %v (a pointer, a slice or an Optional makes it nullable)",
			tool["name"], name, kind, kinds)
	}
	checkBounds(t, tool, name, valueBranch(t, prop, root), bounds)
}

func sameJSONValue(got, want any) bool {
	_, gotBool := got.(bool)
	_, wantBool := want.(bool)
	if gotBool || wantBool {
		return gotBool && wantBool && got == want
	}
	switch w := want.(type) {
	case int:
		g, isNumber := got.(float64)
		return isNumber && g == float64(w)
	case float64:
		g, isNumber := got.(float64)
		return isNumber && g == w
	}
	return reflect.DeepEqual(got, want)
}

func checkBounds(t *testing.T, tool object, name string, schema object, bounds []bound) {
	t.Helper()
	for _, b := range bounds {
		var got any
		if b.key == "default" {
			root, _ := tool["inputSchema"].(object)
			prop := resolve(t, inputProperty(t, tool, name), root)
			var found bool
			if got, found = prop["default"]; !found {
				got = schema["default"]
			}
		} else {
			got = schema[b.key]
		}
		if !sameJSONValue(got, b.want) {
			t.Fatalf("%s.%s: %s must be %s, got %s", tool["name"], name, b.key, jsonText(b.want), jsonText(got))
		}
	}
}

func requiredOf(tool object) []string {
	schema, _ := tool["inputSchema"].(object)
	list, _ := schema["required"].([]any)
	names := []string{}
	for _, item := range list {
		if s, isString := item.(string); isString {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	return names
}

// outputFields returns the property names of the output schema, or of a nested
// object or list item under it.
func outputFields(t *testing.T, tool object, path ...string) map[string]bool {
	t.Helper()
	root, _ := tool["outputSchema"].(object)
	if len(root) == 0 {
		t.Fatalf("%s publishes no outputSchema", tool["name"])
	}
	var schema any = root
	for _, part := range path {
		var found any
		for _, branch := range branches(t, schema, root) {
			if props, isObject := branch["properties"].(object); isObject {
				if prop, has := props[part]; has {
					found = prop
				}
			}
		}
		if found == nil {
			t.Fatalf("%s: the output schema has no field %s", tool["name"], strings.Join(path, "."))
		}
		schema = found
		for _, branch := range branches(t, schema, root) {
			if items, has := branch["items"]; has && items != nil {
				schema = items
				break
			}
		}
	}
	names := map[string]bool{}
	for _, branch := range branches(t, schema, root) {
		props, _ := branch["properties"].(object)
		for key := range props {
			names[key] = true
		}
	}
	return names
}

func checkTitle(t *testing.T, tool object, title string) {
	t.Helper()
	var titles []any
	if v, has := tool["title"]; has && v != nil {
		titles = append(titles, v)
	}
	if hints, isObject := tool["annotations"].(object); isObject {
		if v, has := hints["title"]; has && v != nil {
			titles = append(titles, v)
		}
	}
	if len(titles) == 0 {
		t.Fatalf("%s: no title; CONTRACT.md gives %q", tool["name"], title)
	}
	for _, got := range titles {
		if got != title {
			t.Fatalf("%s: title must be %q, got %s", tool["name"], title, jsonText(titles))
		}
	}
}

func checkReadAnnotations(t *testing.T, tool object) {
	t.Helper()
	hints, _ := tool["annotations"].(object)
	name := tool["name"]
	if hints["readOnlyHint"] != true {
		t.Fatalf("%s: readOnlyHint must be true", name)
	}
	if hints["openWorldHint"] != false {
		t.Fatalf("%s: openWorldHint must be false (it defaults to true)", name)
	}
	if hints["destructiveHint"] != nil {
		t.Fatalf("%s: leave destructiveHint out on a read-only tool", name)
	}
	if v := hints["idempotentHint"]; v != nil && v != false {
		t.Fatalf("%s: leave idempotentHint out on a read-only tool", name)
	}
}

func checkDescription(t *testing.T, tool object) {
	t.Helper()
	text, _ := tool["description"].(string)
	if n := utf8.RuneCountInString(text); n < 40 || n > 400 {
		t.Fatalf("%s: the description must be 40 to 400 characters, it has %d", tool["name"], n)
	}
}

// ------------------------------------------------------------------ the server

// server is one server process and one MCP client session. It starts on first use.
type server struct {
	root    string // the log root with symlinks resolved
	reports string // "" gives the server no --reports
	command []string
	task    string

	once    sync.Once
	session *mcp.ClientSession
	stderr  syncBuffer
	failure string
	tools   map[string]object
}

func newServer(root, reports string, flags ...string) *server {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved, _ = filepath.Abs(root)
	}
	args := []string{"--root", root}
	if reports != "" {
		args = append(args, "--reports", reports)
	}
	return &server{root: resolved, reports: reports, command: append(args, flags...), task: "Task 2"}
}

func (s *server) start() {
	s.once.Do(func() {
		base, tried, err := serverCommand()
		if err != nil {
			s.command, s.failure = tried, err.Error()
			return
		}
		s.command = append(append([]string{}, base...), s.command...)
		cmd := exec.Command(s.command[0], s.command[1:]...)
		cmd.Dir = checksDir
		cmd.Stderr = &s.stderr
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		client := mcp.NewClient(&mcp.Implementation{Name: "log-triage-checks", Version: "0.2.0"}, nil)
		transport := freshTransport{&mcp.CommandTransport{Command: cmd, TerminateDuration: 5 * time.Second}}
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			s.failure = err.Error()
			return
		}
		s.session = session
	})
}

func (s *server) close() {
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
		s.failure = "the server was stopped"
	}
}

func (s *server) require(t *testing.T) *mcp.ClientSession {
	t.Helper()
	s.start()
	if s.session == nil {
		t.Fatal(startFailure(s.command, s.failure, s.stderrTail(15), s.task))
	}
	return s.session
}

func (s *server) stderrTail(n int) string {
	return tail(s.stderr.String(), n)
}

func (s *server) serverInfo(t *testing.T) *mcp.Implementation {
	t.Helper()
	return s.require(t).InitializeResult().ServerInfo
}

func (s *server) instructions(t *testing.T) string {
	t.Helper()
	return s.require(t).InitializeResult().Instructions
}

func (s *server) toolList(t *testing.T) map[string]object {
	t.Helper()
	if s.tools == nil {
		session := s.require(t)
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		tools := map[string]object{}
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				t.Fatalf("tools/list failed: %v", err)
			}
			var m object
			if err := remarshal(tool, &m); err != nil {
				t.Fatalf("tools/list: %v", err)
			}
			tools[tool.Name] = m
		}
		s.tools = tools
	}
	return s.tools
}

func (s *server) tool(t *testing.T, name, task string) object {
	t.Helper()
	tools := s.toolList(t)
	tool, found := tools[name]
	if !found {
		listed := "no tools"
		if len(tools) > 0 {
			listed = fmt.Sprint(sortedKeys(tools))
		}
		where := ""
		if task != "" {
			where = " " + task
		}
		t.Fatalf("%s is not in tools/list (found: %s).%s", name, listed, where)
	}
	return tool
}

// callTool makes one call without failing the test, so it can run on any goroutine.
func (s *server) callTool(session *mcp.ClientSession, name string, arguments map[string]any) (*reply, error) {
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
			return r, nil
		}
		return r, fmt.Errorf("%s got no MCP answer: %v\nLast lines on the server's stderr:\n%s", r.label(), err, s.stderrTail(15))
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
	return r, nil
}

func (s *server) call(t *testing.T, name string, arguments map[string]any) *reply {
	t.Helper()
	r, err := s.callTool(s.require(t), name, arguments)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// callMany calls the same tool with each argument set at once, over one session.
func (s *server) callMany(t *testing.T, name string, arguments []map[string]any) []*reply {
	t.Helper()
	session := s.require(t)
	replies := make([]*reply, len(arguments))
	errs := make([]error, len(arguments))
	var wg sync.WaitGroup
	for i, args := range arguments {
		wg.Go(func() { replies[i], errs[i] = s.callTool(session, name, args) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	return replies
}

// scan calls summarize_logs and returns its scan_id.
func (s *server) scan(t *testing.T, arguments map[string]any) string {
	t.Helper()
	s.tool(t, "summarize_logs", "")
	result := ok(t, s.call(t, "summarize_logs", arguments))
	scanID, isString := result["scan_id"].(string)
	if !isString || !scanIDRE.MatchString(scanID) {
		t.Fatalf("summarize_logs must return a scan_id like s_ and 16 lowercase hex characters, got %s. Task 2 adds it.",
			jsonText(result["scan_id"]))
	}
	return scanID
}

func (s *server) requireTemplate(t *testing.T, uriTemplate string) {
	t.Helper()
	var listed []string
	for _, item := range s.templates(t) {
		template, _ := item["uriTemplate"].(string)
		if template == uriTemplate {
			return
		}
		listed = append(listed, template)
	}
	found := "none"
	if len(listed) > 0 {
		found = fmt.Sprint(listed)
	}
	t.Fatalf("resources/templates/list has no %s (found: %s). Task 5 adds the resources.", uriTemplate, found)
}

func (s *server) resources(t *testing.T) []object {
	t.Helper()
	session := s.require(t)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var listed []object
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			t.Fatalf("resources/list failed: %v", err)
		}
		var m object
		if err := remarshal(resource, &m); err != nil {
			t.Fatalf("resources/list: %v", err)
		}
		listed = append(listed, m)
	}
	return listed
}

func (s *server) templates(t *testing.T) []object {
	t.Helper()
	session := s.require(t)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var listed []object
	for template, err := range session.ResourceTemplates(ctx, nil) {
		if err != nil {
			t.Fatalf("resources/templates/list failed: %v", err)
		}
		var m object
		if err := remarshal(template, &m); err != nil {
			t.Fatalf("resources/templates/list: %v", err)
		}
		listed = append(listed, m)
	}
	return listed
}

func (s *server) read(t *testing.T, uri string) *resourceReply {
	t.Helper()
	session := s.require(t)
	r := &resourceReply{uri: uri}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		var wire *jsonrpc.Error
		if errors.As(err, &wire) {
			r.wireErr = wire
			return r
		}
		t.Fatalf("reading %s got no MCP answer: %v\nLast lines on the server's stderr:\n%s", uri, err, s.stderrTail(15))
	}
	r.contents = result.Contents
	return r
}

// resourceReply is the answer to one resources/read.
type resourceReply struct {
	uri      string
	wireErr  *jsonrpc.Error
	contents []*mcp.ResourceContents
}

// document is the one text content, parsed as JSON.
func (r *resourceReply) document(t *testing.T) any {
	t.Helper()
	if r.wireErr != nil {
		t.Fatalf("reading %s failed: JSON-RPC error %d: %s. Task 5 adds the resources.",
			r.uri, r.wireErr.Code, cut(r.wireErr.Message, 300))
	}
	if len(r.contents) != 1 {
		t.Fatalf("reading %s must give one content, got %d", r.uri, len(r.contents))
	}
	var doc any
	if err := json.Unmarshal([]byte(r.contents[0].Text), &doc); err != nil {
		t.Fatalf("reading %s: the text is not JSON: %q", r.uri, cut(r.contents[0].Text, 200))
	}
	return doc
}

func (r *resourceReply) mimeType() string {
	if len(r.contents) == 0 {
		return ""
	}
	return r.contents[0].MIMEType
}

// freshTransport sets ttlMs to 0 in every reply before the client decodes it.
// The client keeps list and read results for ttlMs, and a second read of the
// same URI must reach the server. The pair itself is checked on raw frames.
type freshTransport struct{ mcp.Transport }

func (f freshTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := f.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return freshConnection{conn}, nil
}

type freshConnection struct{ mcp.Connection }

func (c freshConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if resp, isResponse := msg.(*jsonrpc.Response); isResponse && bytes.Contains(resp.Result, []byte(`"ttlMs"`)) {
		var result map[string]json.RawMessage
		if json.Unmarshal(resp.Result, &result) == nil {
			if _, has := result["ttlMs"]; has {
				result["ttlMs"] = json.RawMessage("0")
				if data, err := json.Marshal(result); err == nil {
					resp.Result = data
				}
			}
		}
	}
	return msg, err
}

// syncBuffer collects a process's stderr while tests read it.
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

// ------------------------------------------------------------------ raw frames

// envelope is the _meta every request carries in protocol 2026-07-28, which has
// no initialize handshake.
var envelope = object{
	"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
	"io.modelcontextprotocol/clientInfo":         object{"name": "log-triage-checks", "version": "2"},
	"io.modelcontextprotocol/clientCapabilities": object{},
}

type rawRequest struct {
	method string
	params object
}

// rawExchange sends server/discover and then each request as one JSON-RPC line,
// and returns the answers to the requests. The client library hides some result
// fields, so this talks to the server's stdin and stdout directly.
func rawExchange(t *testing.T, root, reports string, requests ...rawRequest) []object {
	t.Helper()
	base, tried, err := serverCommand()
	if err != nil {
		t.Fatal(startFailure(tried, err.Error(), "(nothing)", "Task 2"))
	}
	command := append(append([]string{}, base...), "--root", root, "--reports", reports)
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = checksDir
	var stderr syncBuffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(startFailure(command, err.Error(), "(nothing)", "Task 2"))
	}
	lines := make(chan []byte, 16)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	defer func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	var answers []object
	all := append([]rawRequest{{"server/discover", object{}}}, requests...)
	for i, req := range all {
		number := i + 1
		params := object{}
		for key, value := range req.params {
			params[key] = value
		}
		params["_meta"] = envelope
		frame, err := json.Marshal(object{"jsonrpc": "2.0", "id": number, "method": req.method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stdin.Write(append(frame, '\n')); err != nil {
			t.Fatalf("writing %s to the server: %v\nstderr:\n%s", req.method, err, tail(stderr.String(), 15))
		}
		wait := callTimeout
		if number == 1 {
			wait = startTimeout
		}
		var message object
		for message == nil {
			select {
			case line, open := <-lines:
				if !open {
					t.Fatalf("the server closed stdout before answering %s. Command: %s\nstderr:\n%s",
						req.method, shellJoin(command), tail(stderr.String(), 15))
				}
				var m object
				if err := json.Unmarshal(line, &m); err != nil {
					t.Fatalf("the server wrote a line to stdout that is not JSON-RPC: %q", cut(string(line), 200))
				}
				if m["id"] == float64(number) {
					message = m
				}
			case <-time.After(wait):
				t.Fatalf("no answer to %s on the server's stdout", req.method)
			}
		}
		if number > 1 {
			answers = append(answers, message)
		}
	}
	return answers
}

// ------------------------------------------------------------------ shared trees and servers

var (
	sharedMu      sync.Mutex
	sharedServers = map[string]*server{}
	trees         = map[string]tree{}
)

// treeFor builds a tree of expected.json once per run, like a session fixture.
func treeFor(t *testing.T, name string) tree {
	t.Helper()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if built, found := trees[name]; found {
		return built
	}
	built, err := buildTree(name, filepath.Join(workDir, "trees", name))
	if err != nil {
		t.Fatalf("building the temporary log root %q: %v", name, err)
	}
	trees[name] = built
	return built
}

// module holds the servers of one check file, like module-scoped fixtures:
// each file gets its own, each started on first use.
type module string

func (m module) shared(t *testing.T, kind string, create func() *server) *server {
	t.Helper()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	key := string(m) + "/" + kind
	if s, found := sharedServers[key]; found {
		return s
	}
	s := create()
	sharedServers[key] = s
	return s
}

func (m module) reports(t *testing.T, kind string) string {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, string(m)+"-"+kind+"-reports-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// server is the server on s3_logs, writing findings to a temporary folder.
func (m module) server(t *testing.T) *server {
	t.Helper()
	return m.shared(t, "s3_logs", func() *server { return newServer(s3Logs, m.reports(t, "s3_logs")) })
}

func (m module) treeServer(t *testing.T) *server {
	t.Helper()
	root := treeFor(t, "tree").root
	return m.shared(t, "tree", func() *server { return newServer(root, m.reports(t, "tree")) })
}

func (m module) hostileServer(t *testing.T) *server {
	t.Helper()
	root := treeFor(t, "hostile").root
	return m.shared(t, "hostile", func() *server { return newServer(root, m.reports(t, "hostile")) })
}

// shortServer is the server on s3_logs with scan ids that live for 2 seconds.
func (m module) shortServer(t *testing.T) *server {
	t.Helper()
	return m.shared(t, "short", func() *server { return newServer(s3Logs, m.reports(t, "short"), "--scan-ttl", "2") })
}

// servers returns the servers by the root name expected.json uses.
func (m module) servers(t *testing.T) map[string]*server {
	t.Helper()
	return map[string]*server{"s3_logs": m.server(t), "tree": m.treeServer(t), "hostile": m.hostileServer(t)}
}

func closeShared() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	for _, s := range sharedServers {
		s.close()
	}
}

// ownServer starts a server for one test and stops it when the test ends.
func ownServer(t *testing.T, root, reports string, flags ...string) *server {
	t.Helper()
	s := newServer(root, reports, flags...)
	t.Cleanup(s.close)
	return s
}

// ------------------------------------------------------------------ small helpers

// Files of s3_logs that several check files use; no question in the course is about them.
const (
	appFile = "pods/provisioner-2/archive/app-2026-08-30.log.gz"
	phpFile = "vms/web-01/archive/php-fpm-2026-08-30.log.gz"
)

// badArgs is one refused call: its subtest id, its arguments and why it is refused.
type badArgs struct {
	id   string
	args map[string]any
	why  string
}

// lacking returns the names that fields doesn't hold.
func lacking(fields map[string]bool, names ...string) []string {
	var missing []string
	for _, name := range names {
		if !fields[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func remarshal(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}

// jsonValue returns v as encoding/json decodes it into an any.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	var out any
	if err := remarshal(v, &out); err != nil {
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
