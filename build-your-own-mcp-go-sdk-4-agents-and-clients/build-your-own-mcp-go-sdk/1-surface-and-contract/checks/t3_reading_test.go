package checks

// Task 3: reading logs by docs/log-formats.md, seen through a first list_logs.
//
// Times, levels, messages and groups are checked through summarize_logs and
// search_logs in the later check files.

import (
	"reflect"
	"testing"
)

const task3 = "Task 3 registers a first list_logs so the checks can see the formats your reader detects."

type pathFormat struct {
	Path   any
	Format any
}

func listed(t *testing.T, s *server, path *string) []any {
	t.Helper()
	s.tool(t, "list_logs", task3)
	args := map[string]any{}
	if path != nil {
		args["path"] = *path
	}
	r := s.call(t, "list_logs", args)
	if r.wireErr != nil || r.isError {
		t.Fatalf("%s failed: %s", r.label(), r.summary())
	}
	result, _ := r.structured.(map[string]any)
	files, isList := result["files"].([]any)
	if result == nil || !isList {
		t.Fatalf("%s must return an object whose 'files' lists the files (CONTRACT.md, list_logs), got %s",
			r.label(), cut(jsonText(r.structured), 200))
	}
	return files
}

func pathsAndFormats(files []any) []pathFormat {
	got := []pathFormat{}
	for _, f := range files {
		file, _ := f.(map[string]any)
		got = append(got, pathFormat{file["path"], file["format"]})
	}
	return got
}

func TestListLogsIsRegistered(t *testing.T) {
	logServer(t).tool(t, "list_logs", task3)
}

func TestFormatOfOneFile(t *testing.T) {
	for _, path := range expected.Formats.Keys {
		t.Run(path, func(t *testing.T) {
			got := pathsAndFormats(listed(t, logServer(t), &path))
			want := []pathFormat{{path, expected.Formats.Values[path]}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("list_logs on one file must list that file with its format; expected %v, got %v", want, got)
			}
		})
	}
}

func TestFormatsOfAFolderInByteOrder(t *testing.T) {
	got := pathsAndFormats(listed(t, treeServer(t), nil))
	result, _ := calls["list-tree"].Result.(map[string]any)
	files, _ := result["files"].([]any)
	want := pathsAndFormats(files)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list_logs on a folder must list every regular file below it, in byte order of the "+
			"relative path, skipping names that start with '.', each with its format.\nexpected: %v\ngot:      %v",
			want, got)
	}
}
