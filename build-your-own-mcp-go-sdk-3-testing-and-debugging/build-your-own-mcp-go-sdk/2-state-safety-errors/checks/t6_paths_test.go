package checks

// Task 6: path screening, symlinks, size and line caps, unreadable archives, E1 and E2.

import (
	"os"
	"path/filepath"
	"testing"
)

const task6Reads = "Task 6: symlinked entries, unreadable and oversized files are skipped (CONTRACT.md, Reading logs)."
const groupLinesTask = "Task 3 registers group_lines."

var m6 = module("t6")

// pathTools are the tools that take a path, with the other arguments each needs.
var pathTools = []struct {
	name  string
	task  string
	extra map[string]any
}{
	{"list_logs", "", map[string]any{}},
	{"summarize_logs", "", map[string]any{}},
	{"search_logs", "", map[string]any{"pattern": "."}},
	{"read_lines", "Task 4 registers read_lines.", map[string]any{"line": 1}},
}

var outsidePaths = []string{
	"a\x00b.log",
	"app.log\x00",
	"/etc",
	"/",
	"..",
	"../",
	"a/../../b",
	"walk/../../outside/secret.log",
	"{base}/outside/secret.log",
	"link-out.log",
	"link-in.log",
	"linkdir",
	"linkdir/secret.log",
	"walk/zz-link.log",
	"walk/sub-link",
}

var missingPaths = []string{
	"nope",
	"nope/missing.log",
	"walk/missing.log",
	"{root}/missing.log",
}

func hostileCalls(tool string) []string {
	var ids []string
	for _, id := range callsFor(tool, "result") {
		if calls[id].Root == "hostile" {
			ids = append(ids, id)
		}
	}
	return ids
}

func pathArguments(extra map[string]any, path string) map[string]any {
	args := map[string]any{"path": path}
	for key, value := range extra {
		args[key] = value
	}
	return args
}

func TestOutsideTheRootOrThroughASymlink(t *testing.T) {
	for _, tool := range pathTools {
		t.Run(tool.name, func(t *testing.T) {
			for _, raw := range outsidePaths {
				t.Run(raw, func(t *testing.T) {
					hostile := treeFor(t, "hostile")
					s := m6.hostileServer(t)
					s.tool(t, tool.name, tool.task)
					path := fill(raw, map[string]string{"base": hostile.base, "root": hostile.root}).(string)
					r := s.call(t, tool.name, pathArguments(tool.extra, path))
					toolError(t, r, e1(path), hostile.root)
				})
			}
		})
	}
}

func TestMissingPath(t *testing.T) {
	for _, tool := range pathTools {
		t.Run(tool.name, func(t *testing.T) {
			for _, raw := range missingPaths {
				t.Run(raw, func(t *testing.T) {
					hostile := treeFor(t, "hostile")
					s := m6.hostileServer(t)
					s.tool(t, tool.name, tool.task)
					path := fill(raw, map[string]string{"root": hostile.root}).(string)
					r := s.call(t, tool.name, pathArguments(tool.extra, path))
					toolError(t, r, e2(path), hostile.root)
				})
			}
		})
	}
}

func TestScansSkipWhatTheyCannotRead(t *testing.T) {
	for _, id := range hostileCalls("summarize_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, m6.hostileServer(t), id, task6Reads)
		})
	}
}

func TestSearchesSkipWhatTheyCannotRead(t *testing.T) {
	for _, id := range hostileCalls("search_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, m6.hostileServer(t), id, task6Reads)
		})
	}
}

func TestListingLeavesSymlinksOut(t *testing.T) {
	for _, id := range hostileCalls("list_logs") {
		t.Run(id, func(t *testing.T) {
			checkAnswer(t, m6.hostileServer(t), id, task6Reads)
		})
	}
}

// The root itself may sit under symlinks; only the part below it is screened.
func TestALogRootUnderASymlinkedFolderWorks(t *testing.T) {
	hostile := treeFor(t, "hostile")
	running := ownServer(t, filepath.Join(hostile.base, "root-link"), filepath.Join(tempDir(t), "reports"))
	for _, id := range []string{"list-hostile-walk", "summarize-hostile-walk", "search-hostile-walk"} {
		checkAnswer(t, running, id, "CONTRACT.md, Paths: the log root may sit under symlinks.")
	}
}

func TestGroupLinesRefusesAFileReplacedByASymlink(t *testing.T) {
	base := tempDir(t)
	root := filepath.Join(base, "logs")
	outside := filepath.Join(base, "outside.log")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := []byte("2026-01-15T10:00:00.000Z ERROR worker stopped\n")
	if err := os.WriteFile(filepath.Join(root, "app.log"), inside, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("AUDIT_OUTSIDE_MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, args := scanGroup(t, root, "app.log")
	if err := os.Remove(filepath.Join(root, "app.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "app.log")); err != nil {
		t.Fatal(err)
	}
	toolError(t, s.call(t, "group_lines", args), e1("app.log"), s.root)
}

func TestGroupLinesRefusesAParentReplacedByASymlink(t *testing.T) {
	base := tempDir(t)
	root := filepath.Join(base, "logs")
	insideDir := filepath.Join(root, "nested")
	outsideDir := filepath.Join(base, "outside")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := []byte("2026-01-15T10:00:00.000Z ERROR worker stopped\n")
	if err := os.WriteFile(filepath.Join(insideDir, "app.log"), inside, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "app.log"), []byte("AUDIT_OUTSIDE_MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, args := scanGroup(t, root, "nested/app.log")
	if err := os.Rename(insideDir, filepath.Join(root, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, insideDir); err != nil {
		t.Fatal(err)
	}
	toolError(t, s.call(t, "group_lines", args), e1("nested/app.log"), s.root)
}

func scanGroup(t *testing.T, root, path string) (*server, map[string]any) {
	t.Helper()
	s := ownServer(t, root, filepath.Join(tempDir(t), "reports"))
	s.tool(t, "group_lines", groupLinesTask)
	scan := ok(t, s.call(t, "summarize_logs", map[string]any{"path": path, "min_level": "any"}))
	groups, _ := scan["groups"].([]any)
	if len(groups) == 0 {
		t.Fatalf("summarize_logs found no group in %s", jsonText(scan))
	}
	group, _ := groups[0].(map[string]any)
	return s, map[string]any{"scan_id": scan["scan_id"], "group_id": group["group_id"]}
}
