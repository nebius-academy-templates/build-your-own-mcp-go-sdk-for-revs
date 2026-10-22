// Package logs reads log files by the rules of docs/log-formats.md: format
// detection, and the time, level and message of every line.
package logs

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Levels, from low to high.
const (
	LevelUnknown  = "unknown"
	LevelDebug    = "debug"
	LevelInfo     = "info"
	LevelWarning  = "warning"
	LevelError    = "error"
	LevelCritical = "critical"
)

var levelRank = map[string]int{
	LevelUnknown: 0, LevelDebug: 1, LevelInfo: 2, LevelWarning: 3, LevelError: 4, LevelCritical: 5,
}

// LevelRank orders levels from unknown (0) to critical (5).
func LevelRank(level string) int { return levelRank[level] }

// FormatUnknown is the format of a file no sniff pattern matches.
const FormatUnknown = "unknown"

// The whitespace class of Python's re.ASCII \s. Go's \s lacks \v, so the
// patterns spell it out.
const (
	space    = `[\t\n\v\f\r ]`
	nonSpace = `[^\t\n\v\f\r ]`
)

// trimSpace trims the whitespace class from both ends of s.
func trimSpace(s string) string { return strings.Trim(s, "\t\n\v\f\r ") }

// format reads one line of its kind. prevLevel is the level of the line before
// it in the same file ("" on the first line).
type format struct {
	name  string
	sniff *regexp.Regexp
	parse func(text, prevLevel string) parsed
}

// parsed is what a line says about itself, before untimed lines borrow a time.
type parsed struct {
	unix    int64
	timed   bool
	level   string
	message string
}

// formats in detection order.
var formats = []format{
	{"app", regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z (DEBUG|INFO|WARN|ERROR|FATAL) ` + nonSpace + `+ `), parseApp},
	{"nginx-access", regexp.MustCompile(`^` + nonSpace + `+ ` + nonSpace + `+ ` + nonSpace + `+ \[\d{2}/[A-Z][a-z]{2}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}\] "`), parseNginxAccess},
	{"nginx-error", regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} \[[a-z]+\] \d+#\d+: `), parseNginxError},
	{"php-fpm", regexp.MustCompile(`^\[\d{2}-[A-Z][a-z]{2}-\d{4} \d{2}:\d{2}:\d{2}\] [A-Z]+: `), parsePHPFPM},
	{"postgres", regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+ UTC \[\d+\] [A-Z0-9]+: `), parsePostgres},
	{"redis", regexp.MustCompile(`^\d+:[XCSM] \d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2}\.\d+ [.\-*#] `), parseRedis},
	{"syslog", regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2} ` + nonSpace + `+ [^\t\n\v\f\r :]+: `), parseSyslog},
	{"pytest", regexp.MustCompile(`^=== run ` + nonSpace + `+ ===$|::` + nonSpace + `* (PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)`), parsePytest},
	{"gotest", regexp.MustCompile(`^=== run ` + nonSpace + `+ ===$|^` + space + `*(--- (PASS|FAIL|SKIP):|=== (RUN|PAUSE|CONT) )`), parseGotest},
}

var formatsByName = func() map[string]format {
	m := make(map[string]format, len(formats))
	for _, f := range formats {
		m[f.name] = f
	}
	return m
}()

// FormatNames lists the formats in detection order.
func FormatNames() []string {
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.name
	}
	return names
}

// detectLines is how many non-empty lines detection reads.
const detectLines = 50

// Detect returns the format of a file from its first lines. Empty lines don't
// count; the highest number of sniff matches wins, the earlier format on a tie.
func Detect(lines []string) string {
	counts := make([]int, len(formats))
	seen := 0
	for _, line := range lines {
		if seen == detectLines {
			break
		}
		if line == "" {
			continue
		}
		seen++
		for i, f := range formats {
			if f.sniff.MatchString(line) {
				counts[i]++
			}
		}
	}
	best, bestCount := FormatUnknown, 0
	for i, n := range counts {
		if n > bestCount {
			best, bestCount = formats[i].name, n
		}
	}
	return best
}

// shapeless is a line without its format's timestamp and level layout.
func shapeless(text string) parsed {
	return parsed{level: LevelUnknown, message: trimSpace(text)}
}

// ---------------------------------------------------------------- times

var months = map[string]int{
	"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6,
	"Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12,
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// civil returns the Unix time of a wall-clock time at a UTC offset in minutes,
// or false when the date or time doesn't exist.
func civil(year, month, day, hour, minute, second, offset int) (int64, bool) {
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if t.Day() != day || t.Month() != time.Month(month) {
		return 0, false
	}
	return t.Unix() - int64(offset)*60, true
}

// zoneOffset reads a sign, hours and minutes into minutes east of UTC.
func zoneOffset(sign, hours, minutes string) (int, bool) {
	h, m := atoi(hours), atoi(minutes)
	if h > 23 || m > 59 {
		return 0, false
	}
	off := h*60 + m
	if sign == "-" {
		off = -off
	}
	return off, true
}

var windowTime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|([+-])(\d{2}):(\d{2}))?)?$`)

// ParseTime reads the grammar of since, until and run headers: YYYY-MM-DD, or
// YYYY-MM-DDTHH:MM with optional :SS, fraction and zone. Fractions are cut.
func ParseTime(text string) (int64, bool) {
	m := windowTime.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	if m[4] == "" {
		return civil(atoi(m[1]), atoi(m[2]), atoi(m[3]), 0, 0, 0, 0)
	}
	offset := 0
	if m[8] != "" {
		var ok bool
		if offset, ok = zoneOffset(m[8], m[9], m[10]); !ok {
			return 0, false
		}
	}
	return civil(atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4]), atoi(m[5]), atoi(m[6]), offset)
}

// FormatTime writes a Unix time as YYYY-MM-DDTHH:MM:SSZ.
func FormatTime(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05Z")
}

// ---------------------------------------------------------------- the formats

var appLine = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.\d+Z (DEBUG|INFO|WARN|ERROR|FATAL) ` + nonSpace + `+ `)

var appLevels = map[string]string{
	"FATAL": LevelCritical, "ERROR": LevelError, "WARN": LevelWarning, "INFO": LevelInfo, "DEBUG": LevelDebug,
}

func parseApp(text, _ string) parsed {
	m := appLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	unix, ok := civil(atoi(g[1]), atoi(g[2]), atoi(g[3]), atoi(g[4]), atoi(g[5]), atoi(g[6]), 0)
	return parsed{unix: unix, timed: ok, level: appLevels[g[7]], message: text[m[1]:]}
}

var (
	accessLine   = regexp.MustCompile(`^` + nonSpace + `+ ` + nonSpace + `+ ` + nonSpace + `+ \[(\d{2})/([A-Z][a-z]{2})/(\d{4}):(\d{2}):(\d{2}):(\d{2}) ([+-])(\d{2})(\d{2})\] "`)
	accessStatus = regexp.MustCompile(`^ (\d{3})(?: |$)`)
)

func parseNginxAccess(text, _ string) parsed {
	m := accessLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	open := strings.IndexByte(text, '"')
	closing := strings.IndexByte(text[open+1:], '"')
	if closing < 0 {
		return shapeless(text)
	}
	request := text[open+1 : open+1+closing]
	status := accessStatus.FindStringSubmatch(text[open+1+closing+1:])
	if status == nil {
		return shapeless(text)
	}
	code := atoi(status[1])
	level := LevelInfo
	switch {
	case code >= 500:
		level = LevelError
	case code >= 400:
		level = LevelWarning
	}
	message := "- - " + status[1]
	if parts := strings.Split(request, " "); len(parts) == 3 {
		path, _, _ := strings.Cut(parts[1], "?")
		message = parts[0] + " " + path + " " + status[1]
	}
	p := parsed{level: level, message: message}
	month, known := months[g[2]]
	if offset, ok := zoneOffset(g[7], g[8], g[9]); ok && known {
		p.unix, p.timed = civil(atoi(g[3]), month, atoi(g[1]), atoi(g[4]), atoi(g[5]), atoi(g[6]), offset)
	}
	return p
}

var (
	errorLine   = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2}) (\d{2}):(\d{2}):(\d{2}) \[([a-z]+)\] \d+#\d+: `)
	errorConnID = regexp.MustCompile(`^\*\d+ `)
	errorLevels = map[string]string{
		"emerg": LevelCritical, "alert": LevelCritical, "crit": LevelCritical, "error": LevelError,
		"warn": LevelWarning, "notice": LevelInfo, "info": LevelInfo, "debug": LevelDebug,
	}
)

func parseNginxError(text, _ string) parsed {
	m := errorLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	message := text[m[1]:]
	if loc := errorConnID.FindStringIndex(message); loc != nil {
		message = message[loc[1]:]
	}
	unix, ok := civil(atoi(g[1]), atoi(g[2]), atoi(g[3]), atoi(g[4]), atoi(g[5]), atoi(g[6]), 0)
	return parsed{unix: unix, timed: ok, level: levelOf(errorLevels, g[7]), message: message}
}

var (
	phpLine   = regexp.MustCompile(`^\[(\d{2})-([A-Z][a-z]{2})-(\d{4}) (\d{2}):(\d{2}):(\d{2})\] ([A-Z]+): `)
	phpLevels = map[string]string{
		"ALERT": LevelCritical, "ERROR": LevelError, "WARNING": LevelWarning, "NOTICE": LevelInfo, "DEBUG": LevelDebug,
	}
)

func parsePHPFPM(text, _ string) parsed {
	m := phpLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	p := parsed{level: levelOf(phpLevels, g[7]), message: text[m[1]:]}
	if month, known := months[g[2]]; known {
		p.unix, p.timed = civil(atoi(g[3]), month, atoi(g[1]), atoi(g[4]), atoi(g[5]), atoi(g[6]), 0)
	}
	return p
}

var (
	postgresLine   = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})\.\d+ UTC \[\d+\] ([A-Z0-9]+): `)
	postgresLevels = map[string]string{
		"PANIC": LevelCritical, "FATAL": LevelCritical, "ERROR": LevelError, "WARNING": LevelWarning,
		"LOG": LevelInfo, "INFO": LevelInfo, "NOTICE": LevelInfo,
		"DEBUG1": LevelDebug, "DEBUG2": LevelDebug, "DEBUG3": LevelDebug, "DEBUG4": LevelDebug, "DEBUG5": LevelDebug,
	}
	postgresFollowers = map[string]bool{
		"DETAIL": true, "HINT": true, "STATEMENT": true, "CONTEXT": true, "QUERY": true, "LOCATION": true,
	}
)

func parsePostgres(text, prevLevel string) parsed {
	m := postgresLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	level := levelOf(postgresLevels, g[7])
	if postgresFollowers[g[7]] {
		level = prevLevel
		if level == "" {
			level = LevelUnknown
		}
	}
	unix, ok := civil(atoi(g[1]), atoi(g[2]), atoi(g[3]), atoi(g[4]), atoi(g[5]), atoi(g[6]), 0)
	return parsed{unix: unix, timed: ok, level: level, message: strings.TrimLeft(text[m[1]:], " ")}
}

var (
	redisLine   = regexp.MustCompile(`^\d+:[XCSM] (\d{2}) ([A-Z][a-z]{2}) (\d{4}) (\d{2}):(\d{2}):(\d{2})\.\d+ ([.\-*#]) `)
	redisLevels = map[string]string{"#": LevelWarning, "*": LevelInfo, "-": LevelDebug, ".": LevelDebug}
)

func parseRedis(text, _ string) parsed {
	m := redisLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	p := parsed{level: redisLevels[g[7]], message: text[m[1]:]}
	if month, known := months[g[2]]; known {
		p.unix, p.timed = civil(atoi(g[3]), month, atoi(g[1]), atoi(g[4]), atoi(g[5]), atoi(g[6]), 0)
	}
	return p
}

var (
	syslogLine = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.\d+([+-])(\d{2}):(\d{2}) ` + nonSpace + `+ ([^\t\n\v\f\r :]+): `)
	syslogPID  = regexp.MustCompile(`\[\d+\]$`)
)

func parseSyslog(text, _ string) parsed {
	m := syslogLine.FindStringSubmatchIndex(text)
	if m == nil {
		return shapeless(text)
	}
	g := groups(text, m)
	program := syslogPID.ReplaceAllString(g[10], "")
	p := parsed{level: LevelUnknown, message: program + ": " + text[m[1]:]}
	if offset, ok := zoneOffset(g[7], g[8], g[9]); ok {
		p.unix, p.timed = civil(atoi(g[1]), atoi(g[2]), atoi(g[3]), atoi(g[4]), atoi(g[5]), atoi(g[6]), offset)
	}
	return p
}

var (
	runHeader    = regexp.MustCompile(`^=== run (` + nonSpace + `+) ===$`)
	pytestResult = regexp.MustCompile(`::` + nonSpace + `* (PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)`)
	gotestFail   = regexp.MustCompile(`^` + space + `*--- FAIL:`)
	gotestPass   = regexp.MustCompile(`^` + space + `*--- (PASS|SKIP):`)
	gotestStep   = regexp.MustCompile(`^` + space + `*=== (RUN|PAUSE|CONT) `)
)

// header reads a run header line; ok is false when the line isn't one.
func header(text string) (p parsed, ok bool) {
	m := runHeader.FindStringSubmatch(text)
	if m == nil {
		return parsed{}, false
	}
	p = parsed{level: LevelInfo, message: trimSpace(text)}
	p.unix, p.timed = ParseTime(m[1])
	return p, true
}

func parsePytest(text, _ string) parsed {
	if p, ok := header(text); ok {
		return p
	}
	p := shapeless(text)
	result := pytestResult.FindStringSubmatch(text)
	switch {
	case result != nil && (result[1] == "FAILED" || result[1] == "ERROR"),
		strings.HasPrefix(text, "FAILED "), strings.HasPrefix(text, "ERROR "):
		p.level = LevelError
	case result != nil:
		p.level = LevelInfo
	}
	return p
}

func parseGotest(text, _ string) parsed {
	if p, ok := header(text); ok {
		return p
	}
	p := shapeless(text)
	switch {
	case gotestFail.MatchString(text), strings.HasPrefix(text, "FAIL"):
		p.level = LevelError
	case gotestPass.MatchString(text), strings.HasPrefix(text, "ok "):
		p.level = LevelInfo
	case gotestStep.MatchString(text):
		p.level = LevelDebug
	}
	return p
}

// groups returns the submatches of a FindStringSubmatchIndex result.
func groups(text string, m []int) []string {
	out := make([]string, len(m)/2)
	for i := range out {
		if m[2*i] >= 0 {
			out[i] = text[m[2*i]:m[2*i+1]]
		}
	}
	return out
}

func levelOf(table map[string]string, word string) string {
	if level, ok := table[word]; ok {
		return level
	}
	return LevelUnknown
}
