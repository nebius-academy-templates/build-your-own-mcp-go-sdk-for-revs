package checks

// Start the server under test and talk to it over MCP.
//
// Nothing here imports the server. The command comes from LOG_TRIAGE_CMD (a JSON
// array) or is the binary built from <repo>/server; the checks add --root,
// --reports and any other flags as separate arguments.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

var timeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

var (
	checksDir string
	repoRoot  string
	s3Logs    string
	serverDir string
	workDir   string // the built server, the temporary log root and reports folders

	expected facts
	calls    = map[string]call{}
)

// facts is checks/expected.json.
type facts struct {
	Formats orderedStrings `json:"formats"`
	Tree    []treeEntry    `json:"tree"`
	Calls   []call         `json:"calls"`
}

type treeEntry struct {
	Path   string   `json:"path"`
	Copy   string   `json:"copy"`
	Gunzip string   `json:"gunzip"`
	Text   []string `json:"text"`
}

type call struct {
	ID     string         `json:"id"`
	Root   string         `json:"root"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
	Result any            `json:"result"`
}

// orderedStrings is a JSON object of strings that keeps its keys in file order.
type orderedStrings struct {
	Keys   []string
	Values map[string]string
}

func (o *orderedStrings) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("expected an object")
	}
	o.Values = map[string]string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var value string
		if err := dec.Decode(&value); err != nil {
			return err
		}
		o.Keys = append(o.Keys, key)
		o.Values[key] = value
	}
	_, err := dec.Token()
	return err
}

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
	// Log roots and the absolute paths built from them are sent with symlinks
	// resolved, as a server that resolves its root expects them.
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

func startFailure(command []string, failure, stderr string) string {
	return "The server did not start, so this check can't run.\n" +
		"Command: " + shellJoin(command) + "\n" +
		"Error: " + failure + "\n" +
		"Last lines on the server's stderr:\n" + stderr + "\n" +
		"Task 2 creates the server in solution/ (see the practice README). " +
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

// ------------------------------------------------------------------ the log roots

func fillRoot(value any, root string) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, "{root}", root)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = fillRoot(item, root)
		}
		return out
	}
	return value
}

// buildTree writes the temporary log root described in expected.json: copies,
// gunzipped copies and text.
func buildTree(dest string) error {
	for _, entry := range expected.Tree {
		target := filepath.Join(dest, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		var data []byte
		var err error
		switch {
		case entry.Copy != "":
			data, err = os.ReadFile(filepath.Join(s3Logs, filepath.FromSlash(entry.Copy)))
		case entry.Gunzip != "":
			data, err = gunzipFile(filepath.Join(s3Logs, filepath.FromSlash(entry.Gunzip)))
		default:
			data = []byte(strings.Join(entry.Text, "\n") + "\n")
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
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
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(zr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rawLines reads a file as text lines the way Python's str.splitlines does.
func rawLines(t *testing.T, path string) []string {
	t.Helper()
	var data []byte
	var err error
	if strings.HasSuffix(filepath.Base(path), ".gz") {
		data, err = gunzipFile(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	text := strings.ToValidUTF8(string(data), "�")
	var lines []string
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch r {
		case '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '', ' ', ' ':
			lines = append(lines, text[start:i])
			if r == '\r' && strings.HasPrefix(text[i+size:], "\n") {
				size++
			}
			start = i + size
		}
		i += size
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
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

// refusedBySchema checks a call rejected by schema validation as a tool result
// with isError and no structuredContent.
func refusedBySchema(t *testing.T, r *reply, why string) {
	t.Helper()
	if r.wireErr != nil {
		t.Fatalf("%s must come back as a tool error, got %s", r.label(), r.summary())
	}
	if !r.isError {
		t.Fatalf("%s was accepted, but %s; it must be refused before the tool runs", r.label(), why)
	}
	if r.structured != nil {
		t.Fatalf("%s is an error but carries structuredContent", r.label())
	}
}

// toolError checks a call refused by the tool itself: a result with isError true.
func toolError(t *testing.T, r *reply, why string) {
	t.Helper()
	if r.wireErr != nil {
		t.Fatalf("%s must come back as a tool error (a result with isError: true), got %s", r.label(), r.summary())
	}
	if !r.isError {
		t.Fatalf("%s was accepted, but %s", r.label(), why)
	}
	if r.structured != nil {
		t.Fatalf("%s is an error but carries structuredContent", r.label())
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

func checkAnswer(t *testing.T, s *server, callID string) map[string]any {
	t.Helper()
	c := calls[callID]
	args := fillRoot(c.Args, s.root).(map[string]any)
	result := ok(t, s.call(t, c.Tool, args))
	tool := s.tool(t, c.Tool, "")
	if err := validate(tool["outputSchema"], result); err != nil {
		t.Fatalf("%s %s: structuredContent does not match the outputSchema: %v", c.Tool, callID, err)
	}
	if err := same(result, c.Result, c.Tool+" "+callID); err != nil {
		t.Fatal(err)
	}
	return result
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

func checkNullableString(t *testing.T, tool object, name string, bounds ...bound) {
	t.Helper()
	root, _ := tool["inputSchema"].(object)
	prop := inputProperty(t, tool, name)
	kinds := typesOf(t, prop, root)
	if !reflect.DeepEqual(kinds, []string{"null", "string"}) {
		t.Fatalf("%s.%s: type must be string or null, got %v", tool["name"], name, kinds)
	}
	if def := resolve(t, prop, root)["default"]; def != nil {
		t.Fatalf("%s.%s: the default must be null", tool["name"], name)
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

func checkEnum(t *testing.T, tool object, name string, values []string, def string) {
	t.Helper()
	root, _ := tool["inputSchema"].(object)
	prop := inputProperty(t, tool, name)
	kinds := typesOf(t, prop, root)
	if !reflect.DeepEqual(kinds, []string{"string"}) {
		t.Fatalf("%s.%s: type must be exactly 'string', got %v", tool["name"], name, kinds)
	}
	var got []string
	enum, found := valueBranch(t, prop, root)["enum"].([]any)
	for _, v := range enum {
		s, _ := v.(string)
		got = append(got, s)
	}
	want := append([]string{}, values...)
	sort.Strings(got)
	sort.Strings(want)
	if !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("%s.%s: enum must be %v, got %s", tool["name"], name, values, jsonText(enum))
	}
	if def != "" {
		checkBounds(t, tool, name, valueBranch(t, prop, root), []bound{{"default", def}})
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
	root    string
	reports string // "" gives the server no --reports
	flags   []string

	once    sync.Once
	command []string
	session *mcp.ClientSession
	stderr  syncBuffer
	failure string
	tools   map[string]object
}

func newServer(root, reports string, flags ...string) *server {
	return &server{root: root, reports: reports, flags: flags}
}

func (s *server) start() {
	s.once.Do(func() {
		base, tried, err := serverCommand()
		if err != nil {
			s.command, s.failure = tried, err.Error()
			return
		}
		s.command = append([]string{}, base...)
		s.command = append(s.command, "--root", s.root)
		if s.reports != "" {
			s.command = append(s.command, "--reports", s.reports)
		}
		s.command = append(s.command, s.flags...)
		cmd := exec.Command(s.command[0], s.command[1:]...)
		cmd.Dir = checksDir
		cmd.Stderr = &s.stderr
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		client := mcp.NewClient(&mcp.Implementation{Name: "log-triage-checks", Version: "0.1.0"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: 5 * time.Second}, nil)
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
		t.Fatal(startFailure(s.command, s.failure, s.stderrTail(15)))
	}
	return s.session
}

func (s *server) stderrTail(n int) string {
	lines := strings.Split(strings.TrimRight(s.stderr.String(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if text := strings.Join(lines, "\n"); text != "" {
		return text
	}
	return "(nothing)"
}

func (s *server) serverInfo(t *testing.T) *mcp.Implementation {
	t.Helper()
	return s.require(t).InitializeResult().ServerInfo
}

func (s *server) instructions(t *testing.T) string {
	t.Helper()
	return s.require(t).InitializeResult().Instructions
}

func (s *server) protocolVersion(t *testing.T) string {
	t.Helper()
	return s.require(t).InitializeResult().ProtocolVersion
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

func (s *server) call(t *testing.T, name string, arguments map[string]any) *reply {
	t.Helper()
	session := s.require(t)
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
		t.Fatalf("%s got no MCP answer: %v\nLast lines on the server's stderr:\n%s", r.label(), err, s.stderrTail(15))
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

// ------------------------------------------------------------------ shared servers

var (
	sharedMu      sync.Mutex
	sharedServers = map[string]*server{}
	treeOnce      sync.Once
	treeDir       string
	treeErr       error
)

// shared returns the server that create makes under key, making it on first use.
func shared(key string, create func() (*server, error)) (*server, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if s, found := sharedServers[key]; found {
		return s, nil
	}
	s, err := create()
	if err != nil {
		return nil, err
	}
	sharedServers[key] = s
	return s, nil
}

func closeShared() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	for _, s := range sharedServers {
		s.close()
	}
}

func reportsFolder(name string) (string, error) {
	return os.MkdirTemp(workDir, name+"-")
}

// logServer is the server on s3_logs, writing findings to a temporary folder.
func logServer(t *testing.T) *server {
	t.Helper()
	s, err := shared("s3_logs", func() (*server, error) {
		reports, err := reportsFolder("reports")
		return newServer(s3Logs, reports), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// treeRoot is a small log root built from expected.json, for folder-level checks.
func treeRoot(t *testing.T) string {
	t.Helper()
	treeOnce.Do(func() {
		treeDir = filepath.Join(workDir, "tree", "logs root")
		treeErr = buildTree(treeDir)
	})
	if treeErr != nil {
		t.Fatalf("building the temporary log root: %v", treeErr)
	}
	return treeDir
}

// treeServer is the server on the temporary log root.
func treeServer(t *testing.T) *server {
	t.Helper()
	root := treeRoot(t)
	s, err := shared("tree", func() (*server, error) {
		reports, err := reportsFolder("reports")
		return newServer(root, reports), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// servers returns both servers by the root name expected.json uses.
func servers(t *testing.T) map[string]*server {
	t.Helper()
	return map[string]*server{"s3_logs": logServer(t), "tree": treeServer(t)}
}

// ownServer starts a server for one test and stops it when the test ends.
func ownServer(t *testing.T, root, reports string, flags ...string) *server {
	t.Helper()
	s := newServer(root, reports, flags...)
	t.Cleanup(s.close)
	return s
}

// callsFor lists the ids of the expected calls of one tool, in file order.
func callsFor(tool string) []string {
	var ids []string
	for _, c := range expected.Calls {
		if c.Tool == tool {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// ------------------------------------------------------------------ small helpers

// levels are the values of min_level, low to high.
var levels = []string{"any", "debug", "info", "warning", "error", "critical"}

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
