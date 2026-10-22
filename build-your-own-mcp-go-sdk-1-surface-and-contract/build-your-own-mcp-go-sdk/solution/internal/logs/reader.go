package logs

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrOutsideRoot is returned for a path whose location is outside the log root.
var ErrOutsideRoot = errors.New("outside the log root")

// Root is the folder every path argument is resolved under.
type Root struct {
	Dir string // absolute
}

// Target is a resolved path argument.
type Target struct {
	Abs   string // absolute path on disk
	Rel   string // relative to the root, "/"-separated, "." for the root
	IsDir bool
}

// Resolve turns a path argument into a location under the root. "" and "."
// mean the root; an absolute path must lie inside it.
func (r Root) Resolve(arg string) (Target, error) {
	var abs string
	switch {
	case arg == "" || arg == ".":
		abs = r.Dir
	case filepath.IsAbs(arg):
		abs = filepath.Clean(arg)
	default:
		abs = filepath.Join(r.Dir, arg)
	}
	rel, err := filepath.Rel(r.Dir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Target{}, fmt.Errorf("path %q is %w", arg, ErrOutsideRoot)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Target{}, fmt.Errorf("path %q does not exist under the log root", arg)
	}
	return Target{Abs: abs, Rel: filepath.ToSlash(rel), IsDir: info.IsDir()}, nil
}

// Entry is one log file under the root.
type Entry struct {
	Path       string // relative to the root, "/"-separated
	Abs        string
	Size       int64 // on disk
	Compressed bool
}

// Files lists the regular files a target stands for: the file itself, or every
// regular file below the folder in byte order of the relative path, skipping
// names that start with ".".
func (r Root) Files(t Target) ([]Entry, error) {
	if !t.IsDir {
		info, err := os.Stat(t.Abs)
		if err != nil {
			return nil, err
		}
		return []Entry{newEntry(t.Rel, t.Abs, info.Size())}, nil
	}
	var entries []Entry
	err := filepath.WalkDir(t.Abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != t.Abs && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.Dir, path)
		if err != nil {
			return err
		}
		entries = append(entries, newEntry(filepath.ToSlash(rel), path, info.Size()))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func newEntry(rel, abs string, size int64) Entry {
	return Entry{Path: rel, Abs: abs, Size: size, Compressed: strings.HasSuffix(rel, ".gz")}
}

// open returns the decompressed content of a file.
func open(e Entry) (io.ReadCloser, error) {
	f, err := os.Open(e.Abs)
	if err != nil {
		return nil, err
	}
	if !e.Compressed {
		return f, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return readCloser{zr, func() error { zr.Close(); return f.Close() }}, nil
}

type readCloser struct {
	io.Reader
	close func() error
}

func (rc readCloser) Close() error { return rc.close() }

// decode turns bytes into text, one U+FFFD per invalid byte.
func decode(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			sb.WriteRune(utf8.RuneError)
		} else {
			sb.Write(b[:size])
		}
		b = b[size:]
	}
	return sb.String()
}

// splitLines splits decoded content into lines: a "\r" right before "\n" is
// dropped, and a final "\n" doesn't start another line.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// ReadLines returns every line of a file, decompressed and decoded.
func ReadLines(e Entry) ([]string, error) {
	rc, err := open(e)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return splitLines(decode(data)), nil
}

// headLines returns the first lines of a file, enough for detection.
func headLines(e Entry) ([]string, error) {
	rc, err := open(e)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	br := bufio.NewReader(rc)
	var lines []string
	nonEmpty := 0
	for nonEmpty < detectLines {
		raw, err := br.ReadString('\n')
		if raw == "" && err != nil {
			break
		}
		line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		if !strings.HasSuffix(raw, "\n") {
			line = raw
		}
		line = decode([]byte(line))
		lines = append(lines, line)
		if line != "" {
			nonEmpty++
		}
		if err != nil {
			break
		}
	}
	return lines, nil
}

// DetectFile returns the format of a file.
func DetectFile(e Entry) (string, error) {
	lines, err := headLines(e)
	if err != nil {
		return "", err
	}
	return Detect(lines), nil
}

// Line is one line of a file with its time, level and message.
type Line struct {
	Number  int // 1-based
	Text    string
	Unix    int64 // valid when Timed
	Timed   bool
	Level   string
	Message string
}

// ParseLines reads every line of text in the given format. Lines without a time
// of their own take the time of the nearest earlier timed line, and lines before
// the first timed line take that line's time.
func ParseLines(texts []string, formatName string) []Line {
	f := formatsByName[formatName]
	lines := make([]Line, len(texts))
	prevLevel := ""
	firstTimed := -1
	for i, text := range texts {
		p := f.parse(text, prevLevel)
		prevLevel = p.level
		lines[i] = Line{Number: i + 1, Text: text, Unix: p.unix, Timed: p.timed, Level: p.level, Message: p.message}
		if p.timed && firstTimed < 0 {
			firstTimed = i
		}
	}
	if firstTimed < 0 {
		return lines
	}
	last := lines[firstTimed].Unix
	for i := range lines {
		if lines[i].Timed {
			last = lines[i].Unix
			continue
		}
		lines[i].Unix, lines[i].Timed = last, true
	}
	return lines
}

// ReadFile reads and parses one file in its detected format.
func ReadFile(e Entry, formatName string) ([]Line, error) {
	texts, err := ReadLines(e)
	if err != nil {
		return nil, err
	}
	return ParseLines(texts, formatName), nil
}
