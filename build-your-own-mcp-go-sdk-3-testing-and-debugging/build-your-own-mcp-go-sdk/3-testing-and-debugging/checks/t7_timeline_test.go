package checks

// Task 7: timeline, E11, E12, version 0.3.0 and the instructions.

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const task7 = "Task 7 registers timeline."

var m7 = module("t7")

const (
	timelineApp = "pods/provisioner-2/archive/app-2026-08-30.log.gz"
	unknownScan = "s_0123456789abcdef"
)

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func versionTuple(t *testing.T, text string) [3]int {
	t.Helper()
	m := versionRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("the server version must look like 0.3.0, got %q", text)
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v
}

func atLeast(v, min [3]int) bool {
	return slices.Compare(v[:], min[:]) >= 0
}

func TestServerVersionIsAtLeast030(t *testing.T) {
	version := m7.server(t).serverInfo(t).Version
	if !atLeast(versionTuple(t, version), [3]int{0, 3, 0}) {
		t.Fatalf("the server version must be 0.3.0 or later, got %q. %s", version, task7)
	}
}

func TestInstructionsAddTheTimelineSentence(t *testing.T) {
	s := m7.server(t)
	text := s.instructions(t)
	want := p1Instructions + " " + p2Sentence("15 minutes") + " " + p3Sentence
	if !atLeast(versionTuple(t, s.serverInfo(t).Version), [3]int{0, 4, 0}) {
		if text != want {
			t.Fatalf("instructions must be the practice 2 text, a space, and the timeline sentence of CONTRACT.md, "+
				"Instructions.\nexpected: %q\ngot:      %q", want, text)
		}
	} else if !strings.HasPrefix(text, want) {
		t.Fatal("later instructions must keep the practice 3 text first")
	}
	if n := utf8.RuneCountInString(text); n > 1000 {
		t.Fatalf("instructions must be at most 1,000 characters, got %d", n)
	}
}

func TestTimelineTitleAndAnnotations(t *testing.T) {
	tool := m7.server(t).tool(t, "timeline", task7)
	checkTitle(t, tool, "Timeline of matching lines")
	checkReadAnnotations(t, tool)
}

func TestTimelineDescriptionLength(t *testing.T) {
	checkDescription(t, m7.server(t).tool(t, "timeline", task7))
}

func TestTimelineParameters(t *testing.T) {
	tool := m7.server(t).tool(t, "timeline", task7)
	checkPlain(t, tool, "scan_id", "string", bound{"pattern", "^s_[0-9a-f]{16}$"})
	checkNullableString(t, tool, "pattern", bound{"minLength", 1}, bound{"maxLength", 200})
	checkPlain(t, tool, "bucket_minutes", "integer", bound{"minimum", 1}, bound{"maximum", 1440}, bound{"default", 60})
	checkNullableString(t, tool, "value_pattern", bound{"minLength", 1}, bound{"maxLength", 200})
	if req := requiredOf(tool); strings.Join(req, ",") != "scan_id" {
		t.Fatalf("timeline: only scan_id is required, got %v", req)
	}
}

func TestTimelineOutputSchema(t *testing.T) {
	tool := m7.server(t).tool(t, "timeline", task7)
	fields := outputFields(t, tool)
	if missing := lacking(fields, "scan_id", "pattern", "value_pattern", "bucket_minutes", "start", "end", "total", "buckets"); len(missing) > 0 {
		t.Fatalf("timeline: the output schema lacks %v", missing)
	}
	bucket := outputFields(t, tool, "buckets")
	if missing := lacking(bucket, "start", "count", "value_count", "min", "median", "max"); len(missing) > 0 {
		t.Fatalf("timeline: a bucket lacks %v", missing)
	}
}

func TestTimelineRefusesBadArguments(t *testing.T) {
	for _, c := range []badArgs{
		{"no-scan-id", map[string]any{}, "scan_id is required"},
		{"scan-id-shape", map[string]any{"scan_id": "scan-1"}, "scan_id matches ^s_[0-9a-f]{16}$"},
		{"bucket-0", map[string]any{"scan_id": unknownScan, "bucket_minutes": 0}, "bucket_minutes is at least 1"},
		{"bucket-1441", map[string]any{"scan_id": unknownScan, "bucket_minutes": 1441}, "bucket_minutes is at most 1,440"},
		{"pattern-empty", map[string]any{"scan_id": unknownScan, "pattern": ""}, "pattern is at least 1 character"},
		{"value-pattern-long", map[string]any{"scan_id": unknownScan, "value_pattern": strings.Repeat("x", 201)}, "value_pattern is at most 200 characters"},
		{"unknown-argument", map[string]any{"scan_id": unknownScan, "surprise": 1}, "timeline has no parameter named surprise"},
	} {
		t.Run(c.id, func(t *testing.T) {
			s := m7.server(t)
			s.tool(t, "timeline", task7)
			refusedBySchema(t, s.call(t, "timeline", c.args), c.why)
		})
	}
}

func TestTimelineAnswers(t *testing.T) {
	for _, id := range callsFor("timeline", "result") {
		t.Run(id, func(t *testing.T) {
			running := m7.servers(t)[calls[id].Root]
			running.tool(t, "timeline", task7)
			result := checkAnswer(t, running, id, "")
			sum := 0.0
			buckets, _ := result["buckets"].([]any)
			for _, b := range buckets {
				bucket, _ := b.(map[string]any)
				count, _ := bucket["count"].(float64)
				sum += count
			}
			if result["total"] != sum {
				t.Fatalf("timeline %s: total must equal the sum of the bucket counts", id)
			}
		})
	}
}

func TestTimelineRefusals(t *testing.T) {
	for _, id := range callsFor("timeline", "error") {
		t.Run(id, func(t *testing.T) {
			running := m7.servers(t)[calls[id].Root]
			running.tool(t, "timeline", task7)
			checkError(t, running, id)
		})
	}
}

func TestTimelineOnAnUnknownScan(t *testing.T) {
	s := m7.server(t)
	s.tool(t, "timeline", task7)
	toolError(t, s.call(t, "timeline", map[string]any{"scan_id": unknownScan}), e6(unknownScan), s.root)
}

func TestTimelineOnAnExpiredScan(t *testing.T) {
	running := ownServerFor(t, "Task 2 of practice 2", s3Logs, filepath.Join(tempDir(t), "reports"), "--scan-ttl", "2")
	running.tool(t, "timeline", task7)
	scanID := running.scan(t, map[string]any{"path": timelineApp})
	ok(t, running.call(t, "timeline", map[string]any{"scan_id": scanID, "bucket_minutes": 1440}))
	time.Sleep(3 * time.Second)
	toolError(t, running.call(t, "timeline", map[string]any{"scan_id": scanID}), e6(scanID), running.root)
}
