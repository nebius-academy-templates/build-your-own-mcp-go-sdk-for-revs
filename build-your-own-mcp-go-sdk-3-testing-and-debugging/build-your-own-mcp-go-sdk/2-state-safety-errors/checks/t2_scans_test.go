package checks

// Task 2: the scan store: version, instructions, --scan-ttl, scan_id and expires_at.

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

const task2 = "Task 2 adds scan_id and expires_at to summarize_logs."

var m2 = module("t2")

var smallScan = map[string]any{"path": phpFile}

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func versionTuple(t *testing.T, text string) [3]int {
	t.Helper()
	m := versionRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("the server version must look like 0.2.0, got %q", text)
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

func secondsFromNow(t *testing.T, value any) float64 {
	t.Helper()
	text, isString := value.(string)
	if !isString || !timeRE.MatchString(text) {
		t.Fatalf("expires_at must be a UTC time like 2026-01-31T14:00:00Z, got %s", jsonText(value))
	}
	when, err := time.Parse("2006-01-02T15:04:05Z", text)
	if err != nil {
		t.Fatalf("expires_at %q does not read: %v", text, err)
	}
	return time.Until(when).Seconds()
}

func checkInstructions(t *testing.T, running *server, lifetime string) {
	t.Helper()
	text := running.instructions(t)
	want := p1Instructions + " " + p2Sentence(lifetime)
	if !atLeast(versionTuple(t, running.serverInfo(t).Version), [3]int{0, 3, 0}) {
		if text != want {
			t.Fatalf("instructions must be the practice 1 text, a space, and the scan sentence of "+
				"CONTRACT.md, Instructions, with the configured lifetime.\nexpected: %q\ngot:      %q", want, text)
		}
		if n := utf8.RuneCountInString(text); n >= 800 {
			t.Fatalf("instructions must stay under 800 characters, got %d", n)
		}
	} else if !strings.HasPrefix(text, want) {
		t.Fatal("later instructions must keep the practice 2 text first")
	}
}

func TestServerVersionIsAtLeast020(t *testing.T) {
	version := m2.server(t).serverInfo(t).Version
	if !atLeast(versionTuple(t, version), [3]int{0, 2, 0}) {
		t.Fatalf("the server version must be 0.2.0 or later, got %q", version)
	}
}

func TestInstructionsNameTheDefaultLifetime(t *testing.T) {
	checkInstructions(t, m2.server(t), "15 minutes")
}

func TestInstructionsFollowScanTTL(t *testing.T) {
	for _, c := range []struct{ id, seconds, lifetime string }{
		{"exact-minutes", "120", "2 minutes"},
		{"singular-minute", "60", "1 minute"},
		{"partial-minute", "150", "150 seconds"},
		{"under-a-minute", "59", "59 seconds"},
		{"singular-second", "1", "1 second"},
		{"hours-in-minutes", "7200", "120 minutes"},
	} {
		t.Run(c.id, func(t *testing.T) {
			running := ownServer(t, s3Logs, filepath.Join(tempDir(t), "reports"), "--scan-ttl", c.seconds)
			checkInstructions(t, running, c.lifetime)
		})
	}
}

func TestSummarizeLogsOutputSchemaHasTheScanFields(t *testing.T) {
	fields := outputFields(t, m2.server(t).tool(t, "summarize_logs", task2))
	if missing := lacking(fields, "scan_id", "expires_at"); len(missing) > 0 {
		t.Fatalf("summarize_logs: the output schema lacks %v", missing)
	}
}

func scanIDOf(t *testing.T, result map[string]any) string {
	t.Helper()
	scanID, isString := result["scan_id"].(string)
	if !isString || !scanIDRE.MatchString(scanID) {
		t.Fatalf("summarize_logs must return a scan_id of s_ and 16 lowercase hex characters, got %s. %s",
			jsonText(result["scan_id"]), task2)
	}
	return scanID
}

func TestSummarizeLogsReturnsAScanID(t *testing.T) {
	scanIDOf(t, ok(t, m2.server(t).call(t, "summarize_logs", smallScan)))
}

func TestScanIDsAreRandom(t *testing.T) {
	s := m2.server(t)
	var ids []string
	for range 100 {
		ids = append(ids, scanIDOf(t, ok(t, s.call(t, "summarize_logs", smallScan))))
	}
	distinct := map[string]bool{}
	for _, id := range ids {
		distinct[id] = true
	}
	if len(distinct) != 100 {
		t.Fatal("100 scans of the same file must give 100 different scan_ids")
	}
	sorted := slices.Sorted(slices.Values(ids))
	reversed := slices.Clone(sorted)
	slices.Reverse(reversed)
	if slices.Equal(ids, sorted) || slices.Equal(ids, reversed) {
		t.Fatal("scan_ids come out in sorted order; they must be random, not a counter or a clock")
	}
	shared := 0
	for i := 1; i < len(ids); i++ {
		shared = max(shared, commonPrefix(ids[i-1], ids[i]))
	}
	if shared >= 12 {
		t.Fatalf("two scan_ids in a row share their first %d characters; they must be random", shared)
	}
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func TestExpiresAtIsNowPlusTheDefaultLifetime(t *testing.T) {
	result := ok(t, m2.server(t).call(t, "summarize_logs", smallScan))
	scanIDOf(t, result)
	left := secondsFromNow(t, result["expires_at"])
	if left < 900-3 || left > 900+2 {
		t.Fatalf("expires_at must be the call time plus 900 seconds (the default --scan-ttl), "+
			"within two seconds; it is %.0f seconds from now", left)
	}
}

func TestExpiresAtFollowsScanTTL(t *testing.T) {
	result := ok(t, m2.shortServer(t).call(t, "summarize_logs", smallScan))
	left := secondsFromNow(t, result["expires_at"])
	if left < -1-2 || left > 2+2 {
		t.Fatalf("with --scan-ttl 2, expires_at must be the call time plus 2 seconds, within two seconds; "+
			"it is %.0f seconds from now", left)
	}
}

func TestTheSameCallTwiceGivesTwoScans(t *testing.T) {
	s := m2.server(t)
	first := scanIDOf(t, ok(t, s.call(t, "summarize_logs", smallScan)))
	second := scanIDOf(t, ok(t, s.call(t, "summarize_logs", smallScan)))
	if first == second {
		t.Fatal("every summarize_logs call makes a new scan with its own scan_id")
	}
}
