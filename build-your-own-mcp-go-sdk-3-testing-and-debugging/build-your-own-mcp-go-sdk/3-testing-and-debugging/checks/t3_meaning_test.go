package checks

// Task 3: your meaning tests against facts.json, the whole suite, and the facts on your server.

import (
	"os"
	"path"
	"path/filepath"
	"testing"
)

const task3 = "Task 3 writes the meaning tests; see facts.json."

var m3 = module("t3")

func TestMeaningTestsRunAndPass(t *testing.T) {
	checkLayer(t, "meaning", 3, task3)
}

func TestTheWholeSuitePassesWithAtLeastTenTests(t *testing.T) {
	needOwnProject(t)
	run := studentSuite(t)
	var failed []string
	passed := 0
	for _, name := range run.order {
		switch run.leaves[name] {
		case "pass":
			passed++
		case "fail":
			failed = append(failed, name)
		}
	}
	if run.err != nil || len(failed) > 0 {
		t.Fatalf("cd solution && go test -count=1 ./... must pass; failed: %v\n%s", failed[:min(len(failed), 10)], tail(run.output, 25))
	}
	if passed < 10 {
		t.Fatalf("your suite must hold at least 10 tests across its layers, it has %d. %s", passed, task3)
	}
}

func TestTheServerAgreesWithFactsJSON(t *testing.T) {
	for _, fact := range factsDoc.Files {
		file, _ := fact["path"].(string)
		t.Run(path.Base(file), func(t *testing.T) {
			s := m3.server(t)
			result := ok(t, s.call(t, "summarize_logs", map[string]any{"path": file, "min_level": "any", "limit": 3}))
			where := "summarize_logs on " + file
			for _, c := range []struct{ got, key string }{
				{"lines_scanned", "lines"}, {"by_level", "by_level"}, {"first_seen", "first_seen"}, {"last_seen", "last_seen"},
			} {
				if err := same(result[c.got], fact[c.key], where+": "+c.got); err != nil {
					t.Fatal(err)
				}
			}
			var got []any
			groups, _ := result["groups"].([]any)
			for _, g := range groups {
				group, _ := g.(map[string]any)
				got = append(got, map[string]any{"group_id": group["group_id"], "count": group["count"]})
			}
			if err := same(jsonValue(t, got), fact["top_groups"], where+": the first three groups"); err != nil {
				t.Fatal(err)
			}
			if window, has := fact["window"].(map[string]any); has {
				result := ok(t, s.call(t, "summarize_logs", map[string]any{
					"path": file, "since": window["since"], "until": window["until"], "min_level": "any"}))
				if err := same(result["lines_in_window"], window["lines_in_window"],
					where+" from "+jsonScalar(window["since"])+" to "+jsonScalar(window["until"])+": lines_in_window"); err != nil {
					t.Fatal(err)
				}
			}
			if headers, has := fact["run_headers"]; has {
				found := ok(t, s.call(t, "search_logs", map[string]any{"path": file, "pattern": `^=== run \S+ ===$`, "limit": 1}))
				if err := same(found["total"], headers, "search_logs for run headers in "+file+": total"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// ignoredInCopies are the summary fields a .gz file and its plain copy may differ in.
var ignoredInCopies = map[string]bool{"scan_id": true, "expires_at": true, "path": true}

func TestAGzFileReadsLikeItsPlainCopy(t *testing.T) {
	for _, file := range factsDoc.GzVsPlain {
		t.Run(file, func(t *testing.T) {
			base := tempDir(t)
			root := filepath.Join(base, "logs")
			name := path.Base(file)
			if err := makeEntry(treeEntry{Path: "logs/gz/" + name, Copy: file}, base); err != nil {
				t.Fatal(err)
			}
			if err := makeEntry(treeEntry{Path: "logs/plain/" + name[:len(name)-len(".gz")], Gunzip: file}, base); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal(err)
			}
			running := ownServer(t, root, filepath.Join(base, "reports"))
			results := map[string]map[string]any{}
			for _, side := range []string{"gz", "plain"} {
				full := ok(t, running.call(t, "summarize_logs", map[string]any{"path": side, "min_level": "any", "limit": 100}))
				groups, _ := full["groups"].([]any)
				for _, g := range groups {
					group, _ := g.(map[string]any)
					delete(group, "files")
					if example, isObject := group["example"].(map[string]any); isObject {
						delete(example, "file")
					}
				}
				kept := map[string]any{}
				for key, value := range full {
					if !ignoredInCopies[key] {
						kept[key] = value
					}
				}
				results[side] = kept
			}
			if err := same(results["plain"], results["gz"], "summarize_logs on the plain copy of "+name+" against the .gz"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
