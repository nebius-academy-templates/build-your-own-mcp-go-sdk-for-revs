package checks

// Task 7: save_finding and the --read-only switch.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const task7 = "Task 7 registers save_finding."

var evidence = []any{
	map[string]any{"file": "pods/provisioner-2/archive/app-2026-08-30.log.gz", "line": 37, "note": "the first warning of the day"},
	map[string]any{"file": "vms/web-01/archive/php-fpm-2026-08-30.log.gz", "line": 1, "note": "a restart right before it"},
}

func good() map[string]any {
	return map[string]any{"title": "A title", "severity": "low", "summary": "A summary.", "evidence": evidence[:1]}
}

// withChange returns good() with change applied; a nil value removes the key.
func withChange(change map[string]any) map[string]any {
	args := good()
	for key, value := range change {
		if value == nil {
			delete(args, key)
		} else {
			args[key] = value
		}
	}
	return args
}

// firstEvidence returns a copy of the first evidence item with change applied.
func firstEvidence(change map[string]any) map[string]any {
	item := map[string]any{}
	for key, value := range evidence[0].(map[string]any) {
		item[key] = value
	}
	for key, value := range change {
		item[key] = value
	}
	return item
}

func rendered(title, severity, saved, summary string, items []any) string {
	lines := []string{"# " + title, "", "- Severity: " + severity, "- Saved: " + saved, "", summary, "",
		"## Evidence", ""}
	for _, e := range items {
		item := e.(map[string]any)
		lines = append(lines, fmt.Sprintf("- `%v` line %v: %v", item["file"], item["line"], item["note"]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func nearNow(text string) bool {
	when, err := time.Parse("2006-01-02T15:04:05Z", text)
	if err != nil {
		return false
	}
	return time.Since(when).Abs() < 120*time.Second
}

// readOnlyServer is the server on s3_logs with --read-only.
func readOnlyServer(t *testing.T) *server {
	t.Helper()
	s, err := shared("read-only", func() (*server, error) {
		reports, err := reportsFolder("read-only-reports")
		return newServer(s3Logs, reports, "--read-only"), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func folderEntries(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

func TestSaveFindingTitleAndAnnotations(t *testing.T) {
	s := logServer(t)
	tool := s.tool(t, "save_finding", task7)
	checkTitle(t, tool, "Save a finding")
	hints, _ := tool["annotations"].(object)
	destructive, has := hints["destructiveHint"]
	version := versionTuple(t, s.serverInfo(t).Version)
	if !atLeast(version, [3]int{0, 2, 0}) {
		if !has || destructive != true {
			t.Fatal("save_finding: destructiveHint must be true in version 0.1 because an existing report is overwritten")
		}
	} else if _, isBool := destructive.(bool); !has || !isBool {
		t.Fatal("save_finding: destructiveHint must be an explicit boolean in version 0.2 or later")
	}
	if v, has := hints["openWorldHint"]; !has || v != false {
		t.Fatal("save_finding: openWorldHint must be false, written out")
	}
	if v := hints["readOnlyHint"]; v != nil && v != false {
		t.Fatal("save_finding: readOnlyHint must be false")
	}
	if v := hints["idempotentHint"]; v != nil && v != false {
		t.Fatal("save_finding: idempotentHint must be false")
	}
}

func TestSaveFindingDescriptionLength(t *testing.T) {
	checkDescription(t, logServer(t).tool(t, "save_finding", task7))
}

func TestSaveFindingParameters(t *testing.T) {
	tool := logServer(t).tool(t, "save_finding", task7)
	checkPlain(t, tool, "title", "string", bound{"minLength", 3}, bound{"maxLength", 120})
	checkEnum(t, tool, "severity", []string{"low", "medium", "high", "critical"}, "")
	checkPlain(t, tool, "summary", "string", bound{"minLength", 1}, bound{"maxLength", 4000})
	checkPlain(t, tool, "evidence", "array", bound{"minItems", 1}, bound{"maxItems", 20})
	if req := requiredOf(tool); strings.Join(req, ",") != "evidence,severity,summary,title" {
		t.Fatalf("save_finding: title, severity, summary and evidence are required, got %v", req)
	}
	root, _ := tool["inputSchema"].(object)
	props0, _ := root["properties"].(object)
	var items []any
	for _, branch := range branches(t, props0["evidence"], root) {
		if it, has := branch["items"]; has && it != nil {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		t.Fatal("save_finding.evidence: the schema must describe its items")
	}
	item := resolve(t, items[0], root)
	props, _ := item["properties"].(object)
	required, _ := item["required"].([]any)
	var names []string
	for _, r := range required {
		s, _ := r.(string)
		names = append(names, s)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"file", "line", "note"}) {
		t.Fatalf("save_finding.evidence items: file, line and note are required, got %s", jsonText(item["required"]))
	}
	for _, field := range []struct {
		name   string
		kind   string
		bounds []bound
	}{
		{"file", "string", []bound{{"maxLength", 500}}},
		{"line", "integer", []bound{{"minimum", 1}}},
		{"note", "string", []bound{{"minLength", 1}, {"maxLength", 300}}},
	} {
		prop, has := props[field.name]
		if !has {
			t.Fatalf("save_finding.evidence items have no field %q", field.name)
		}
		if kinds := typesOf(t, prop, root); !reflect.DeepEqual(kinds, []string{field.kind}) {
			t.Fatalf("save_finding.evidence[].%s: type must be exactly %q, got %v", field.name, field.kind, kinds)
		}
		for _, b := range field.bounds {
			got := resolve(t, prop, root)[b.key]
			if !sameJSONValue(got, b.want) {
				t.Fatalf("save_finding.evidence[].%s: %s must be %v, got %s", field.name, b.key, b.want, jsonText(got))
			}
		}
	}
}

func TestSaveFindingOutputSchema(t *testing.T) {
	fields := outputFields(t, logServer(t).tool(t, "save_finding", task7))
	if missing := lacking(fields, "name", "path", "created"); len(missing) > 0 {
		t.Fatalf("save_finding: the output schema lacks %v", missing)
	}
}

func TestSaveFindingRefusesBadArguments(t *testing.T) {
	for _, c := range []struct {
		id     string
		change map[string]any
		why    string
	}{
		{"title-short", map[string]any{"title": "ab"}, "title is at least 3 characters"},
		{"title-long", map[string]any{"title": strings.Repeat("x", 121)}, "title is at most 120 characters"},
		{"severity-unknown", map[string]any{"severity": "urgent"}, "severity is low, medium, high or critical"},
		{"summary-empty", map[string]any{"summary": ""}, "summary is at least 1 character"},
		{"summary-long", map[string]any{"summary": strings.Repeat("x", 4001)}, "summary is at most 4,000 characters"},
		{"evidence-empty", map[string]any{"evidence": []any{}}, "evidence has at least 1 item"},
		{"evidence-21", map[string]any{"evidence": repeatItem(evidence[0], 21)}, "evidence has at most 20 items"},
		{"evidence-line-0", map[string]any{"evidence": []any{firstEvidence(map[string]any{"line": 0})}}, "an evidence line is 1 or more"},
		{"evidence-no-note", map[string]any{"evidence": []any{map[string]any{"file": evidence[0].(map[string]any)["file"], "line": 3}}}, "an evidence item needs a note"},
		{"evidence-note-empty", map[string]any{"evidence": []any{firstEvidence(map[string]any{"note": ""})}}, "an evidence note is at least 1 character"},
		{"evidence-note-long", map[string]any{"evidence": []any{firstEvidence(map[string]any{"note": strings.Repeat("x", 301)})}}, "an evidence note is at most 300 characters"},
		{"no-summary", map[string]any{"summary": nil}, "summary is required"},
		{"unknown-argument", map[string]any{"surprise": 1}, "save_finding has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "save_finding", task7)
			refusedBySchema(t, s.call(t, "save_finding", withChange(c.change)), c.why)
			if written, _ := filepath.Glob(filepath.Join(s.reports, "a-title*")); len(written) > 0 {
				t.Fatal("a refused save_finding call wrote a file")
			}
		})
	}
}

func repeatItem(item any, n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = item
	}
	return out
}

var savedRE = regexp.MustCompile(`(?m)^- Saved: (.*)$`)

func TestSaveFindingWritesTheMarkdownFile(t *testing.T) {
	s := logServer(t)
	s.tool(t, "save_finding", task7)
	title := "Warnings climb after the morning restart"
	summary := "The warnings start right after a restart.\nThey stop by the evening."
	before := folderEntries(t, s.reports)
	result := ok(t, s.call(t, "save_finding", map[string]any{
		"title": title, "severity": "medium", "summary": summary, "evidence": evidence,
	}))
	slug := "warnings-climb-after-the-morning-restart"
	if result["name"] != slug {
		t.Fatalf("save_finding: name must be the slug %q, got %s", slug, jsonText(result["name"]))
	}
	if result["path"] != slug+".md" {
		t.Fatalf("save_finding: path must be %q, got %s", slug+".md", jsonText(result["path"]))
	}
	created, isString := result["created"].(string)
	if !isString || !timeRE.MatchString(created) || !nearNow(created) {
		t.Fatalf("save_finding: created must be the current UTC time as YYYY-MM-DDTHH:MM:SSZ, got %s", jsonText(result["created"]))
	}
	var written []string
	for name := range folderEntries(t, s.reports) {
		if !before[name] {
			written = append(written, name)
		}
	}
	sort.Strings(written)
	if !reflect.DeepEqual(written, []string{slug + ".md"}) {
		t.Fatalf("save_finding must write exactly %s.md under --reports, it wrote %v", slug, written)
	}
	data, err := os.ReadFile(filepath.Join(s.reports, slug+".md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	saved := savedRE.FindStringSubmatch(content)
	if saved == nil {
		t.Fatalf("the finding has no '- Saved: <time>' line:\n%s", content)
	}
	if saved[1] != created {
		t.Fatalf("the Saved time %q must equal created %q", saved[1], created)
	}
	if want := rendered(title, "medium", created, summary, evidence); content != want {
		t.Fatalf("the finding file must be exactly:\n%q\ngot:\n%q", want, content)
	}
}

func TestSaveFindingNamesTheFileAfterTheTitle(t *testing.T) {
	for _, c := range []struct{ id, title, slug string }{
		{"punctuation", "Newsletter queue stalls on Mondays!", "newsletter-queue-stalls-on-mondays"},
		{"leading-and-trailing", "  --Font loading glitches, 3 of them--  ", "font-loading-glitches-3-of-them"},
		{"no-letters", "!!!", "finding"},
		{"non-ascii", "Ünïcode Title 42", "n-code-title-42"},
		{"ascii-only-lowercase", "İstanbul newsletter", "stanbul-newsletter"},
		{"cut-then-trim", strings.Repeat("a", 59) + " b", strings.Repeat("a", 59)},
		{"cut-mid-word", "Thumbnails render slowly for large albums after every nightly rebuild",
			"thumbnails-render-slowly-for-large-albums-after-every-nightl"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := logServer(t)
			s.tool(t, "save_finding", task7)
			result := ok(t, s.call(t, "save_finding", withChange(map[string]any{"title": c.title})))
			if result["name"] != c.slug {
				t.Fatalf("the slug of %q must be %q, got %s", c.title, c.slug, jsonText(result["name"]))
			}
			if info, err := os.Stat(filepath.Join(s.reports, c.slug+".md")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("%s.md was not written under --reports", c.slug)
			}
		})
	}
}

func TestSaveFindingCreatesTheReportsFolder(t *testing.T) {
	reports := filepath.Join(t.TempDir(), "new", "reports folder")
	s := ownServer(t, s3Logs, reports)
	s.tool(t, "save_finding", task7)
	ok(t, s.call(t, "save_finding", good()))
	s.close()
	if info, err := os.Stat(filepath.Join(reports, "a-title.md")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("save_finding must create a missing --reports folder")
	}
}

func TestReadOnlyRemovesOnlySaveFinding(t *testing.T) {
	s := logServer(t)
	s.tool(t, "save_finding", task7)
	var want []string
	for _, name := range sortedKeys(s.toolList(t)) {
		if name != "save_finding" {
			want = append(want, name)
		}
	}
	hidden := sortedKeys(readOnlyServer(t).toolList(t))
	if !reflect.DeepEqual(hidden, want) {
		t.Fatalf("with --read-only the tools must be %v, got %v", want, hidden)
	}
}

func TestReadOnlyRefusesASaveFindingCall(t *testing.T) {
	logServer(t).tool(t, "save_finding", task7)
	ro := readOnlyServer(t)
	r := ro.call(t, "save_finding", good())
	if r.wireErr == nil {
		t.Fatalf("with --read-only a save_finding call must fail as an unknown-tool JSON-RPC error, got %s", r.summary())
	}
	if entries := folderEntries(t, ro.reports); len(entries) > 0 {
		t.Fatal("with --read-only nothing may be written")
	}
}
