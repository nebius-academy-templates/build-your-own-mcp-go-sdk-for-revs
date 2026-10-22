package checks

// Task 5: resources: triage://formats, triage://reports, the two templates, not-found, freshness.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const task5 = "Task 5 adds the resources."

var m5 = module("t5")

var (
	smallAnyScan   = map[string]any{"path": phpFile, "min_level": "any"}
	resourceProofs = []any{map[string]any{"file": phpFile, "line": 2, "note": "the pool starts"}}
)

const validFinding = "# Worker pool restarts at dawn\n\n- Severity: low\n- Saved: 2026-01-15T10:00:00Z\n\nA note.\n\n## Evidence\n\n- `a.log` line 1: one\n"

var emptyReports = map[string]any{"count": float64(0), "reports": []any{}}

func byURI(items []object, key string) map[string]object {
	found := map[string]object{}
	for _, item := range items {
		name, _ := item[key].(string)
		found[name] = item
	}
	return found
}

func resourceFinding(title string) map[string]any {
	return map[string]any{"title": title, "severity": "high", "summary": "Seen twice.", "evidence": resourceProofs}
}

// stockedServer is a server whose reports folder already holds files the list
// must leave out, and one it keeps.
func stockedServer(t *testing.T) *server {
	t.Helper()
	return m5.shared(t, "stocked", func() *server {
		base, err := os.MkdirTemp(workDir, "stocked-")
		if err != nil {
			t.Fatal(err)
		}
		reports := filepath.Join(base, "reports")
		steps := []error{
			os.Mkdir(reports, 0o755),
			os.WriteFile(filepath.Join(reports, "README.md"), []byte("# Reports\n\nFindings saved by the server.\n"), 0o644),
			os.WriteFile(filepath.Join(reports, ".hidden.md"), []byte(validFinding), 0o644),
			os.WriteFile(filepath.Join(reports, "notes.txt"), []byte(validFinding), 0o644),
			os.WriteFile(filepath.Join(reports, "a.b.md"), []byte(validFinding), 0o644),
			os.WriteFile(filepath.Join(base, "elsewhere.md"), []byte(validFinding), 0o644),
			os.Symlink(filepath.Join(base, "elsewhere.md"), filepath.Join(reports, "linked.md")),
			os.WriteFile(filepath.Join(reports, "worker-pool-restarts-at-dawn.md"), []byte(validFinding), 0o644),
		}
		for _, err := range steps {
			if err != nil {
				t.Fatal(err)
			}
		}
		return newServer(s3Logs, reports)
	})
}

func TestResourcesListTheTwoDocuments(t *testing.T) {
	listed := byURI(m5.server(t).resources(t), "uri")
	for _, uri := range []string{"triage://formats", "triage://reports"} {
		item, found := listed[uri]
		if !found {
			t.Fatalf("resources/list must include %s, found %v. %s", uri, sortedKeys(listed), task5)
		}
		if item["mimeType"] != "application/json" {
			t.Fatalf("%s: mimeType must be application/json, got %s", uri, jsonText(item["mimeType"]))
		}
	}
}

func TestResourceTemplatesListTheTwoTemplates(t *testing.T) {
	listed := byURI(m5.server(t).templates(t), "uriTemplate")
	for _, c := range []struct{ uri, mime string }{
		{"triage://reports/{name}", "text/markdown"},
		{"triage://scans/{scan_id}", "application/json"},
	} {
		item, found := listed[c.uri]
		if !found {
			t.Fatalf("resources/templates/list must include %s, found %v. %s", c.uri, sortedKeys(listed), task5)
		}
		if item["mimeType"] != c.mime {
			t.Fatalf("%s: mimeType must be %s, got %s", c.uri, c.mime, jsonText(item["mimeType"]))
		}
	}
}

func TestFormatsDocument(t *testing.T) {
	reply := m5.server(t).read(t, "triage://formats")
	if got := reply.document(t); !reflect.DeepEqual(got, expected.FormatsDocument) {
		t.Fatalf("triage://formats must list the nine formats in order, each with its levels, timestamp and "+
			"the first sample line of docs/log-formats.md (CONTRACT.md, Resources).\nexpected: %s\ngot:      %s",
			jsonText(expected.FormatsDocument), jsonText(got))
	}
	if reply.mimeType() != "application/json" {
		t.Fatalf("triage://formats: mimeType %q", reply.mimeType())
	}
}

func TestReportsDocumentIsEmptyWithoutReports(t *testing.T) {
	running := ownServer(t, s3Logs, filepath.Join(tempDir(t), "not created yet"))
	reply := running.read(t, "triage://reports")
	if got := reply.document(t); !reflect.DeepEqual(got, emptyReports) {
		t.Fatalf(`triage://reports with no reports folder must be {"count": 0, "reports": []}, got %s`, jsonText(got))
	}
	if reply.mimeType() != "application/json" {
		t.Fatalf("triage://reports: mimeType %q", reply.mimeType())
	}
}

func TestSavedFindingsAreListedAndReadable(t *testing.T) {
	s := m5.server(t)
	s.tool(t, "save_finding", "Practice 1 registers save_finding.")
	if empty := s.read(t, "triage://reports").document(t); !reflect.DeepEqual(empty, emptyReports) {
		t.Fatalf("triage://reports on an empty folder: %s", jsonText(empty))
	}
	zeta := ok(t, s.call(t, "save_finding", resourceFinding("Zeta cache misses")))
	alphaArgs := resourceFinding("Alpha queue backlog")
	alphaArgs["severity"] = "low"
	alpha := ok(t, s.call(t, "save_finding", alphaArgs))
	listed := s.read(t, "triage://reports").document(t)
	want := jsonValue(t, map[string]any{"count": 2, "reports": []any{
		map[string]any{"name": "alpha-queue-backlog", "title": "Alpha queue backlog", "severity": "low", "created": alpha["created"]},
		map[string]any{"name": "zeta-cache-misses", "title": "Zeta cache misses", "severity": "high", "created": zeta["created"]},
	}})
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("triage://reports after two saves:\nexpected: %s\ngot:      %s", jsonText(want), jsonText(listed))
	}
	reply := s.read(t, "triage://reports/zeta-cache-misses")
	if reply.wireErr != nil {
		t.Fatalf("triage://reports/zeta-cache-misses failed: %d %s", reply.wireErr.Code, reply.wireErr.Message)
	}
	content, err := os.ReadFile(filepath.Join(s.reports, "zeta-cache-misses.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.contents) != 1 || reply.contents[0].Text != string(content) {
		t.Fatal("triage://reports/{name} must return the saved file exactly")
	}
	if reply.mimeType() != "text/markdown" {
		t.Fatalf("triage://reports/{name}: mimeType %q", reply.mimeType())
	}
}

func TestReportsListOnlyFindings(t *testing.T) {
	listed := stockedServer(t).read(t, "triage://reports").document(t)
	want := jsonValue(t, map[string]any{"count": 1, "reports": []any{map[string]any{
		"name": "worker-pool-restarts-at-dawn", "title": "Worker pool restarts at dawn",
		"severity": "low", "created": "2026-01-15T10:00:00Z"}}})
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("triage://reports lists regular *.md files with the finding lines, and leaves out dot files, "+
			"symlinks, other names and markdown without those lines.\nexpected: %s\ngot:      %s", jsonText(want), jsonText(listed))
	}
}

func TestReadingAReportTheListDoesNotShow(t *testing.T) {
	for _, name := range []string{"README", "linked", ".hidden", "notes", "missing-finding", "a.b"} {
		t.Run(name, func(t *testing.T) {
			s := stockedServer(t)
			s.requireTemplate(t, "triage://reports/{name}")
			uri := "triage://reports/" + name
			notFound(t, s.read(t, uri), uri)
		})
	}
}

func TestAURINoTemplateMatches(t *testing.T) {
	s := m5.server(t)
	s.requireTemplate(t, "triage://reports/{name}")
	notFound(t, s.read(t, "triage://nothing/here"), "triage://nothing/here")
}

func TestScanDocument(t *testing.T) {
	s := m5.server(t)
	result := ok(t, s.call(t, "summarize_logs", smallAnyScan))
	uri := "triage://scans/" + jsonScalar(result["scan_id"])
	reply := s.read(t, uri)
	if !reflect.DeepEqual(reply.document(t), any(result)) {
		t.Fatalf("%s must hold the summarize_logs result of that scan", uri)
	}
	if reply.mimeType() != "application/json" {
		t.Fatalf("%s: mimeType %q", uri, reply.mimeType())
	}
}

// jsonScalar is a string value as it is, and anything else as JSON text.
func jsonScalar(v any) string {
	if s, isString := v.(string); isString {
		return s
	}
	return jsonText(v)
}

func TestUnknownScanDocument(t *testing.T) {
	s := m5.server(t)
	s.requireTemplate(t, "triage://scans/{scan_id}")
	uri := "triage://scans/s_0123456789abcdef"
	notFound(t, s.read(t, uri), uri)
}

func TestExpiredScanDocument(t *testing.T) {
	s := m5.shortServer(t)
	uri := "triage://scans/" + s.scan(t, smallAnyScan)
	if s.read(t, uri).wireErr != nil {
		t.Fatalf("%s must be readable right after the scan", uri)
	}
	time.Sleep(3 * time.Second)
	notFound(t, s.read(t, uri), uri)
}

func TestReadOnlyKeepsTheResources(t *testing.T) {
	running := ownServer(t, s3Logs, filepath.Join(tempDir(t), "reports"), "--read-only")
	if _, found := byURI(running.resources(t), "uri")["triage://reports"]; !found {
		t.Fatal("--read-only removes only save_finding")
	}
	if got := running.read(t, "triage://reports").document(t); !reflect.DeepEqual(got, emptyReports) {
		t.Fatalf("triage://reports under --read-only: %s", jsonText(got))
	}
}

func TestFreshnessOnTheRawFrames(t *testing.T) {
	methods := []string{"resources/list", "resources/templates/list", "resources/read"}
	answers := rawExchange(t, s3Logs, filepath.Join(tempDir(t), "reports"),
		rawRequest{"resources/list", object{}},
		rawRequest{"resources/templates/list", object{}},
		rawRequest{"resources/read", object{"uri": "triage://formats"}},
	)
	for i, answer := range answers {
		result, isObject := answer["result"].(map[string]any)
		if !isObject {
			t.Fatalf("%s failed on the raw connection: %s", methods[i], jsonText(answer["error"]))
		}
		if result["ttlMs"] != float64(30000) || result["cacheScope"] != "private" {
			t.Fatalf("%s: the result must carry ttlMs 30000 and cacheScope 'private' (CONTRACT.md, Resources), "+
				"got ttlMs=%s, cacheScope=%s", methods[i], jsonText(result["ttlMs"]), jsonText(result["cacheScope"]))
		}
	}
}
