package logs

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPatternReplacesInOrder(t *testing.T) {
	for _, c := range []struct{ message, want string }{
		{"retry 3 of 5 for order 7f3a9c2e-1b4d-4e8a-9c3f-2d1e6b7a8c90 from 192.0.2.10:44321",
			"retry <n> of <n> for order <uuid> from <ip>"},
		{"bind() to 0.0.0.0:80 failed (98: Address already in use)", "bind() to <ip> failed (<n>: Address already in use)"},
		{"client 0xDEADBEEF hash deadbeefcafe", "client <n>xDEADBEEF hash <hex>"},
		{"1:signal-handler (1768471452) Received SIGTERM", "<n>:signal-handler (<hex>) Received SIGTERM"},
		{"took 0.004s at 12.5%", "took <n>s at <n>%"},
		{"привет 42 мир", "привет <n> мир"},
	} {
		if got := Pattern(c.message); got != c.want {
			t.Errorf("Pattern(%q) = %q, want %q", c.message, got, c.want)
		}
	}
}

func TestPatternCollapsesWhitespace(t *testing.T) {
	for _, c := range []struct{ message, want string }{
		{"  charge   failed\tfor  client", "charge failed for client"},
		{"a\v\fb\r\nc  ", "a b c"},
		{"ok  \texample.com/shop\t0.004s", "ok example.com/shop <n>s"},
	} {
		if got := Pattern(c.message); got != c.want {
			t.Errorf("Pattern(%q) = %q, want %q", c.message, got, c.want)
		}
	}
}

func TestPatternCutsTheMessageThenThePattern(t *testing.T) {
	got := Pattern("slow query " + strings.Repeat("x", 1100))
	if want := "slow query " + strings.Repeat("x", 189); got != want {
		t.Errorf("a 1,111-character message: pattern of %d characters, want %d", utf8.RuneCountInString(got), utf8.RuneCountInString(want))
	}
	wide := Pattern(strings.Repeat("й", 1100))
	if n := utf8.RuneCountInString(wide); n != 200 || !utf8.ValidString(wide) {
		t.Errorf("a message of 1,100 two-byte letters: pattern of %d runes (valid UTF-8: %v), want 200", n, utf8.ValidString(wide))
	}
	// The first cut comes before the replacements: digits past character 1,000 never become <n>.
	tail := Pattern(strings.Repeat("a", 999) + " 12345")
	if strings.Contains(tail, "<n>") {
		t.Errorf("the message is cut to 1,000 characters before numbers are replaced, got %q", tail[len(tail)-10:])
	}
}

func TestGroupIDIsTheStartOfASHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("app\nwarning\nretry <n> of <n> for order <uuid> from <ip>"))
	want := hex.EncodeToString(sum[:])[:12]
	if want != "33ddc1fe54a2" {
		t.Fatalf("the reference hash is %s", want)
	}
	if got := GroupID("app", "warning", "retry <n> of <n> for order <uuid> from <ip>"); got != want {
		t.Errorf("GroupID = %q, want %q", got, want)
	}
	if GroupID("app", "error", "same") == GroupID("app", "warning", "same") {
		t.Error("the level is part of the group_id")
	}
}
