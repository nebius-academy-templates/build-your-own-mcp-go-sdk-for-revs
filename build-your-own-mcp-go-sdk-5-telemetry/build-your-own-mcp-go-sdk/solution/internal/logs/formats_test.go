package logs

import (
	"strings"
	"testing"
)

// wantLine is what one line of a small file should read as. An empty time means
// the file has no time at all.
type wantLine struct {
	time, level, message, pattern, groupID string
}

// checkFile detects the format of lines, reads them, and compares every line
// with want, grouping included.
func checkFile(t *testing.T, format string, lines []string, want []wantLine) {
	t.Helper()
	if got := Detect(lines); got != format {
		t.Fatalf("Detect = %q, want %q", got, format)
	}
	got := ParseLines(lines, format)
	if len(got) != len(want) {
		t.Fatalf("ParseLines returned %d lines, want %d", len(got), len(want))
	}
	for i, w := range want {
		l := got[i]
		n := i + 1
		gotTime := ""
		if l.Timed {
			gotTime = FormatTime(l.Unix)
		}
		if gotTime != w.time {
			t.Errorf("line %d: time %q, want %q", n, gotTime, w.time)
		}
		if l.Level != w.level {
			t.Errorf("line %d: level %q, want %q", n, l.Level, w.level)
		}
		if l.Message != w.message {
			t.Errorf("line %d: message %q, want %q", n, l.Message, w.message)
		}
		pattern := Pattern(l.Message)
		if pattern != w.pattern {
			t.Errorf("line %d: pattern %q, want %q", n, pattern, w.pattern)
		}
		if id := GroupID(format, l.Level, pattern); id != w.groupID {
			t.Errorf("line %d: group_id %q, want %q", n, id, w.groupID)
		}
	}
}

func TestAppFormat(t *testing.T) {
	long := strings.Repeat("x", 1100)
	checkFile(t, "app", []string{
		"2026-01-15T10:04:12.345Z WARN billing retry 3 of 5 for order 7f3a9c2e-1b4d-4e8a-9c3f-2d1e6b7a8c90 from 192.0.2.10:44321",
		"2026-01-15T10:04:13.999Z ERROR billing   charge   failed\tfor  client 0xDEADBEEF hash deadbeefcafe",
		"2026-01-15T10:04:14.500Z TRACE billing an unlisted level word",
		"2026-01-15T10:04:15.000Z INFO greeter привет 42 мир",
		"2026-01-15T10:04:16.000Z INFO db slow query " + long,
	}, []wantLine{
		{"2026-01-15T10:04:12Z", "warning", "retry 3 of 5 for order 7f3a9c2e-1b4d-4e8a-9c3f-2d1e6b7a8c90 from 192.0.2.10:44321",
			"retry <n> of <n> for order <uuid> from <ip>", "33ddc1fe54a2"},
		{"2026-01-15T10:04:13Z", "error", "  charge   failed\tfor  client 0xDEADBEEF hash deadbeefcafe",
			"charge failed for client <n>xDEADBEEF hash <hex>", "d288f6427fda"},
		{"2026-01-15T10:04:13Z", "unknown", "2026-01-15T10:04:14.500Z TRACE billing an unlisted level word",
			"<n>-<n>-<n>T<n>:<n>:<n>Z TRACE billing an unlisted level word", "d0dead41d05f"},
		{"2026-01-15T10:04:15Z", "info", "привет 42 мир", "привет <n> мир", "01529d0da3e4"},
		{"2026-01-15T10:04:16Z", "info", "slow query " + long, "slow query " + strings.Repeat("x", 189), "d4c2c0861519"},
	})
}

func TestNginxAccessFormat(t *testing.T) {
	checkFile(t, "nginx-access", []string{
		`192.0.2.10 - - [15/Jan/2026:10:04:12 +0100] "GET /catalog?page=2 HTTP/1.1" 200 512 "-" "curl/8.5.0"`,
		`192.0.2.10 - - [15/Jan/2026:10:04:13 -0130] "\x16\x03" 400 150 "-" "-"`,
		`192.0.2.10 - - [15/Jan/2026:10:04:14 +0000] "GET /health HTTP/1.1" 503`,
	}, []wantLine{
		{"2026-01-15T09:04:12Z", "info", "GET /catalog 200", "GET /catalog <n>", "538e4c29e569"},
		{"2026-01-15T11:34:13Z", "warning", "- - 400", "- - <n>", "bfa0a90ef849"},
		{"2026-01-15T10:04:14Z", "error", "GET /health 503", "GET /health <n>", "3c46eed710c6"},
	})
}

func TestNginxErrorFormat(t *testing.T) {
	checkFile(t, "nginx-error", []string{
		"2026/01/15 10:04:12 [warn] 41#41: *7 an upstream response is buffered to a temporary file",
		"2026/01/15 10:04:13 [emerg] 41#41: bind() to 0.0.0.0:80 failed (98: Address already in use)",
	}, []wantLine{
		{"2026-01-15T10:04:12Z", "warning", "an upstream response is buffered to a temporary file",
			"an upstream response is buffered to a temporary file", "3bd48ce6310b"},
		{"2026-01-15T10:04:13Z", "critical", "bind() to 0.0.0.0:80 failed (98: Address already in use)",
			"bind() to <ip> failed (<n>: Address already in use)", "83140bb4d9a8"},
	})
}

func TestPHPFPMFormat(t *testing.T) {
	checkFile(t, "php-fpm", []string{
		"[15-Jan-2026 10:04:12] NOTICE: fpm is running, pid 38",
		"[31-Feb-2026 10:04:13] WARNING: [pool www] seems busy",
	}, []wantLine{
		{"2026-01-15T10:04:12Z", "info", "fpm is running, pid 38", "fpm is running, pid <n>", "62b09c437369"},
		{"2026-01-15T10:04:12Z", "warning", "[pool www] seems busy", "[pool www] seems busy", "ceb290c77a90"},
	})
}

func TestPostgresFormat(t *testing.T) {
	checkFile(t, "postgres", []string{
		"2026-01-15 10:04:12.345 UTC [2231] DETAIL:  a detail line with nothing before it",
		"2026-01-15 10:04:13.000 UTC [2231] ERROR:  deadlock detected",
		"2026-01-15 10:04:13.000 UTC [2231] HINT:  See server log for query details.",
		"2026-01-15 10:04:14.000 UTC [2232] LOG:  checkpoint complete",
	}, []wantLine{
		{"2026-01-15T10:04:12Z", "unknown", "a detail line with nothing before it", "a detail line with nothing before it", "2373ca066fb5"},
		{"2026-01-15T10:04:13Z", "error", "deadlock detected", "deadlock detected", "5025cead45e0"},
		{"2026-01-15T10:04:13Z", "error", "See server log for query details.", "See server log for query details.", "8bd7e7583a2f"},
		{"2026-01-15T10:04:14Z", "info", "checkpoint complete", "checkpoint complete", "2ccd5fdc5598"},
	})
}

func TestRedisFormat(t *testing.T) {
	checkFile(t, "redis", []string{
		"1:signal-handler (1768471452) Received SIGTERM scheduling shutdown...",
		"1:M 15 Jan 2026 10:04:12.345 - Accepted 192.0.2.10:50000",
		"1:M 15 Jan 2026 10:04:13.001 * Background saving started by pid 41",
	}, []wantLine{
		{"2026-01-15T10:04:12Z", "unknown", "1:signal-handler (1768471452) Received SIGTERM scheduling shutdown...",
			"<n>:signal-handler (<hex>) Received SIGTERM scheduling shutdown...", "8b846eee8129"},
		{"2026-01-15T10:04:12Z", "debug", "Accepted 192.0.2.10:50000", "Accepted <ip>", "6df934109198"},
		{"2026-01-15T10:04:13Z", "info", "Background saving started by pid 41", "Background saving started by pid <n>", "7bfaf9d86945"},
	})
}

func TestSyslogFormat(t *testing.T) {
	checkFile(t, "syslog", []string{
		"2026-01-15T10:04:12.5-05:30 myhost cron[7][8]: job started",
		"2026-01-15T10:04:13.000000+00:00 myhost sshd[812]: Connection closed by 192.0.2.10 port 50122",
	}, []wantLine{
		{"2026-01-15T15:34:12Z", "unknown", "cron[7]: job started", "cron[<n>]: job started", "775320fb57ef"},
		{"2026-01-15T10:04:13Z", "unknown", "sshd: Connection closed by 192.0.2.10 port 50122",
			"sshd: Connection closed by <ip> port <n>", "504577a36b8a"},
	})
}

func TestPytestFormat(t *testing.T) {
	checkFile(t, "pytest", []string{
		"=== run 2026-01-15T10:00Z ===",
		"tests/test_cart.py::test_add PASSED",
		"FAILED tests/test_cart.py::test_remove PASSED",
		"   FAILED tests/test_cart.py::test_indented - not a result at the start",
		"tests/test_cart.py::test_skip SKIPPED (no network)",
	}, []wantLine{
		{"2026-01-15T10:00:00Z", "info", "=== run 2026-01-15T10:00Z ===", "=== run <n>-<n>-<n>T<n>:<n>Z ===", "324d505f63ef"},
		{"2026-01-15T10:00:00Z", "info", "tests/test_cart.py::test_add PASSED", "tests/test_cart.py::test_add PASSED", "e71a9fe27029"},
		{"2026-01-15T10:00:00Z", "error", "FAILED tests/test_cart.py::test_remove PASSED",
			"FAILED tests/test_cart.py::test_remove PASSED", "59c0ddfa5eaa"},
		{"2026-01-15T10:00:00Z", "unknown", "FAILED tests/test_cart.py::test_indented - not a result at the start",
			"FAILED tests/test_cart.py::test_indented - not a result at the start", "b38cf9f35d29"},
		{"2026-01-15T10:00:00Z", "info", "tests/test_cart.py::test_skip SKIPPED (no network)",
			"tests/test_cart.py::test_skip SKIPPED (no network)", "3fda9464f343"},
	})
}

func TestGotestFormat(t *testing.T) {
	checkFile(t, "gotest", []string{
		"=== run yesterday ===",
		"=== RUN   TestCart",
		"    --- FAIL: TestCart/add (0.01s)",
		"    FAIL indented is not a result",
		"ok  \texample.com/shop\t0.004s",
	}, []wantLine{
		{"", "info", "=== run yesterday ===", "=== run yesterday ===", "c8eb13dde41d"},
		{"", "debug", "=== RUN   TestCart", "=== RUN TestCart", "e35e78378e72"},
		{"", "error", "--- FAIL: TestCart/add (0.01s)", "--- FAIL: TestCart/add (<n>s)", "412f338effd7"},
		{"", "unknown", "FAIL indented is not a result", "FAIL indented is not a result", "9553fe86cc51"},
		{"", "info", "ok  \texample.com/shop\t0.004s", "ok example.com/shop <n>s", "6a3746714853"},
	})
}
