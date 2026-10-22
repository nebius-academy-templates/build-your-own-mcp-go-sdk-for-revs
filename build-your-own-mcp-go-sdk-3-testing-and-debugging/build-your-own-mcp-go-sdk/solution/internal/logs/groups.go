package logs

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	uuidRE  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	ipRE    = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b`)
	hexRE   = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	numRE   = regexp.MustCompile(`\d+(\.\d+)?`)
	spaceRE = regexp.MustCompile(space + `+`)
)

// Pattern turns a message into the shape its group is keyed by.
func Pattern(message string) string {
	p := CutRunes(message, 1000)
	p = uuidRE.ReplaceAllLiteralString(p, "<uuid>")
	p = ipRE.ReplaceAllLiteralString(p, "<ip>")
	p = hexRE.ReplaceAllLiteralString(p, "<hex>")
	p = numRE.ReplaceAllLiteralString(p, "<n>")
	p = strings.Trim(spaceRE.ReplaceAllLiteralString(p, " "), " ")
	return CutRunes(p, 200)
}

// GroupID is the first 12 hex characters of the SHA-256 of format, level and
// pattern, one per line.
func GroupID(format, level, pattern string) string {
	sum := sha256.Sum256([]byte(format + "\n" + level + "\n" + pattern))
	return hex.EncodeToString(sum[:])[:12]
}

// CutRunes returns s cut to at most n runes.
func CutRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
