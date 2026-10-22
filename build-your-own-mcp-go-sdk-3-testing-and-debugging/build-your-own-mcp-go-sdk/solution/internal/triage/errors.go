package triage

import (
	"errors"
	"fmt"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
)

// toolError is a refusal whose text the contract fixes (CONTRACT.md section 5).
// The caller sees its text as it is.
type toolError struct{ text string }

func (e *toolError) Error() string { return e.text }

// quote puts a value between plain double quotes, with nothing escaped.
func quote(value string) string { return `"` + value + `"` }

// errOutside is E1.
func errOutside(path string) error {
	return &toolError{"path " + quote(path) + " is outside the log root. Pass a path relative to the log root; " +
		`list_logs with path "." shows what is there.`}
}

// errMissing is E2.
func errMissing(path string) error {
	return &toolError{"path " + quote(path) + " does not exist under the log root. Call list_logs to see what is there."}
}

// unexpectedText is what a caller sees for an error the contract doesn't name.
const unexpectedText = "The server could not complete this call because of an internal problem. " +
	"Try again, or try another path or file."

// contractError gives a refused path its contract text and keeps the other
// contract errors. Anything else is unexpected: the caller gets a generic text
// and ok is false.
func contractError(err error) (_ error, ok bool) {
	var refused *paths.Error
	if errors.As(err, &refused) {
		if errors.Is(refused.Err, paths.ErrMissing) {
			return errMissing(refused.Path), true
		}
		return errOutside(refused.Path), true
	}
	var known *toolError
	if errors.As(err, &known) {
		return known, true
	}
	return &toolError{unexpectedText}, false
}

// errBadTime is E3.
func errBadTime(field string) error {
	return &toolError{field + " must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z."}
}

// errWindowOrder is E4.
func errWindowOrder() error {
	return &toolError{"until must be later than since."}
}

// errBadPattern is E5.
func errBadPattern(field string) error {
	return &toolError{field + " is not a valid regular expression. Escape special characters such as ( [ . * " +
		"with a backslash, or search for a plain word."}
}

// errUnknownScan is E6.
func errUnknownScan(scanID string) error {
	return &toolError{"scan " + quote(scanID) + " is unknown or has expired. Call summarize_logs again for a new scan_id."}
}

// errGroupNotInScan is E7.
func errGroupNotInScan(groupID, scanID string) error {
	return &toolError{"group " + quote(groupID) + " is not in scan " + quote(scanID) + ". Use a group_id from that scan's groups."}
}

// errPastTheEnd is E8.
func errPastTheEnd(line int, file string, total int) error {
	return &toolError{fmt.Sprintf("line %d is past the end of %s, which has %d lines.", line, quote(file), total)}
}

// errExists is E9.
func errExists(name string) error {
	return &toolError{"a finding named " + quote(name) + " already exists. Choose a different title."}
}

// errFolder is E10.
func errFolder(path string) error {
	return &toolError{quote(path) + " is a folder. This needs one file; call list_logs to pick one."}
}

// errOneCaptureGroup is E11.
func errOneCaptureGroup() error {
	return &toolError{`value_pattern must contain exactly one capture group, for example "size=(\d+)".`}
}

// errTooManyBuckets is E12.
func errTooManyBuckets(n int64) error {
	return &toolError{fmt.Sprintf("the window needs %d buckets and the limit is %d. Pass a larger bucket_minutes.", n, bucketLimit)}
}
