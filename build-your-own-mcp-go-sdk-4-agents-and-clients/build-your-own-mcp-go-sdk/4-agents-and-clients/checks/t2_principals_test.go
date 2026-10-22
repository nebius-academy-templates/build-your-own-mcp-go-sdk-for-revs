package checks

// Task 2: callers from tokens: scan ownership, the read-only caller and E13, --read-only over tokens.

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

const task2 = "Task 2 makes the caller come from the token."

var m2 = module("t2")

func windowScan() map[string]any { return calls["group-lines-app-window"].Scan }

func windowGroup() string { return calls["group-lines-app-window"].GroupID }

func finding(title string) map[string]any {
	return map[string]any{
		"title": title, "severity": "low", "summary": "A finding the checks save over HTTP.",
		"evidence": []any{map[string]any{
			"file": "pods/provisioner-2/archive/app-2026-08-30.log.gz", "line": 1, "note": "first line",
		}},
	}
}

func reportNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAScanWorksFromASecondTokenOfItsOwner(t *testing.T) {
	s := m2.server(t)
	checkAnswer(t, s, "group-lines-app-window", owner, owner2)
	checkAnswer(t, s, "timeline-app-window", owner, owner2)
}

func TestTheScanResourceWorksForItsOwner(t *testing.T) {
	s := m2.server(t)
	scanID := s.scan(t, windowScan(), owner)
	wireErr, contents := s.read(t, "triage://scans/"+scanID, owner2)
	if wireErr != nil {
		t.Fatalf("reading triage://scans/%s with another token of the same caller failed: %d %s. %s",
			scanID, wireErr.Code, cut(wireErr.Message, 200), task2)
	}
	if len(contents) == 0 {
		t.Fatalf("triage://scans/%s gave no contents", scanID)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(contents[0].Text), &document); err != nil || document["scan_id"] != scanID {
		t.Fatalf("triage://scans/%s must hold that scan's summary", scanID)
	}
}

func TestAnotherCallerGetsE6(t *testing.T) {
	s := m2.server(t)
	scanID := s.scan(t, windowScan(), owner)
	toolError(t, s.call(t, "group_lines", map[string]any{"scan_id": scanID, "group_id": windowGroup()}, other), e6(scanID))
	toolError(t, s.call(t, "timeline", map[string]any{"scan_id": scanID}, other), e6(scanID))
	wireErr, _ := s.read(t, "triage://scans/"+scanID, other)
	if wireErr == nil || wireErr.Code != notFoundCode {
		got := "a result"
		if wireErr != nil {
			got = jsonText(wireErr.Code)
		}
		t.Fatalf("another caller reading triage://scans/%s must get the resource-not-found error (%d); got %s. %s",
			scanID, notFoundCode, got, task2)
	}
}

func TestTheWriterSeesEveryTool(t *testing.T) {
	names := m2.server(t).toolNames(t, owner)
	var missing []string
	for _, want := range expected.Tools.Writer {
		if !slices.Contains(names, want) {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("a caller with a writing token must see %v in tools/list", missing)
	}
}

func TestTheReadOnlyCallerDoesNotSeeSaveFinding(t *testing.T) {
	names := m2.server(t).toolNames(t, viewer)
	if slices.Contains(names, "save_finding") {
		t.Fatalf("a read-only caller must not see save_finding in tools/list (CONTRACT.md, section 7); got %v. %s",
			names, task2)
	}
	var missing []string
	for _, want := range expected.Tools.ReadOnly {
		if !slices.Contains(names, want) {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("a read-only caller must still see %v", missing)
	}
}

func TestTheReadOnlyCallerGetsE13(t *testing.T) {
	s := m2.server(t)
	before := reportNames(t, s.reports)
	toolError(t, s.call(t, "save_finding", finding("Read-only caller tries to write"), viewer), e13)
	if after := reportNames(t, s.reports); !slices.Equal(after, before) {
		t.Fatalf("a refused save_finding must write nothing; the folder went from %v to %v", before, after)
	}
}

func TestTheReadOnlyCallerScansForItself(t *testing.T) {
	s := m2.server(t)
	scanID := s.scan(t, windowScan(), viewer)
	ok(t, s.call(t, "group_lines", map[string]any{"scan_id": scanID, "group_id": windowGroup()}, viewer))
	toolError(t, s.call(t, "group_lines", map[string]any{"scan_id": scanID, "group_id": windowGroup()}, owner), e6(scanID))
}

func TestToolsListIsPrivate(t *testing.T) {
	s := m2.server(t)
	for _, token := range []string{owner, viewer} {
		result := s.rpc(t, "tools/list", nil, token, "", nil).result(t)
		if result["cacheScope"] != "private" {
			t.Fatalf("tools/list must carry cacheScope \"private\", because the list depends on the caller "+
				"(CONTRACT.md, section 4); got %s for token %s", jsonText(result["cacheScope"]), token)
		}
	}
}

func TestScanCapsArePerCaller(t *testing.T) {
	s := m2.server(t)
	kept := s.scan(t, windowScan(), owner)
	first := s.scan(t, windowScan(), other)
	for range 20 {
		s.scan(t, windowScan(), other)
	}
	toolError(t, s.call(t, "group_lines", map[string]any{"scan_id": first, "group_id": windowGroup()}, other), e6(first))
	ok(t, s.call(t, "group_lines", map[string]any{"scan_id": kept, "group_id": windowGroup()}, owner))
}

func TestAFindingSavedOverHTTPIsListed(t *testing.T) {
	s := m2.server(t)
	saved := ok(t, s.call(t, "save_finding", finding("Saved over HTTP by the checks"), owner))
	wireErr, contents := s.read(t, "triage://reports", owner)
	if wireErr != nil {
		t.Fatalf("reading triage://reports failed: %s", cut(wireErr.Message, 200))
	}
	var listed struct {
		Reports []struct {
			Name string `json:"name"`
		} `json:"reports"`
	}
	if len(contents) == 0 || json.Unmarshal([]byte(contents[0].Text), &listed) != nil {
		t.Fatal("triage://reports must hold a JSON document")
	}
	var names []string
	for _, report := range listed.Reports {
		names = append(names, report.Name)
	}
	if name, _ := saved["name"].(string); !slices.Contains(names, name) {
		t.Fatalf("triage://reports must list the finding saved over HTTP; got %v", names)
	}
}

func TestReadOnlyFlagWinsOverTokens(t *testing.T) {
	running := ownHTTPServer(t, withTokens(), "--read-only")
	names := running.toolNames(t, owner)
	if slices.Contains(names, "save_finding") {
		t.Fatalf("with --read-only no caller sees save_finding, whatever its token; got %v", names)
	}
	raw := running.rpc(t, "tools/call", map[string]any{"name": "save_finding", "arguments": finding("Not registered")}, owner, "", nil)
	refused(t, raw, "save_finding isn't registered with --read-only")
	if result, isResult := raw.message["result"].(map[string]any); isResult {
		content, _ := result["content"].([]any)
		for _, block := range content {
			if m, isObject := block.(map[string]any); isObject && m["text"] == e13 {
				t.Fatal("with --read-only, a call to save_finding is the SDK's unknown-tool refusal, not E13 " +
					"(CONTRACT.md, section 7)")
			}
		}
	}
	if left := reportNames(t, running.reports); len(left) > 0 {
		t.Fatalf("with --read-only nothing may be written; found %v", left)
	}
}
