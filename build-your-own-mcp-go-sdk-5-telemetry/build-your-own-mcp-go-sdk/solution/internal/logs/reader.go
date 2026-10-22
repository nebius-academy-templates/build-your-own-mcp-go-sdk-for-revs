package logs

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/telemetry"
)

// Limits of what a file may hold.
const (
	MaxFileBytes = 64 << 20 // decompressed; a larger file is skipped
	MaxLineRunes = 65_536   // a longer line is cut
)

// Why a file can't be read.
var (
	ErrUnreadable = errors.New("unreadable")
	ErrTooLarge   = errors.New("too large")
)

// Root is the folder every path argument is resolved under.
type Root struct {
	paths.Root
}

// Target is a resolved path argument.
type Target struct {
	Abs   string // absolute path on disk
	Rel   string // relative to the root, "/"-separated, "." for the root
	IsDir bool
}

// Resolve screens a path argument and turns it into a location under the root.
// "" and "." mean the root; an absolute path must lie inside it.
func (r Root) Resolve(arg string) (Target, error) {
	abs, rel, info, err := r.Screen(arg)
	if err != nil {
		return Target{}, err
	}
	return Target{Abs: abs, Rel: rel, IsDir: info.IsDir()}, nil
}

// Entry is one log file under the root.
type Entry struct {
	Path       string // relative to the root, "/"-separated
	Abs        string
	Size       int64 // on disk, as stat reports it
	Compressed bool
	Symlink    bool // a symlink met while walking; never followed
}

// Files lists what a target stands for: the file itself, or every regular file
// and every symlink below the folder, in byte order of the relative path,
// skipping names that start with ".". Symlinks are marked and not followed.
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
		link := d.Type()&fs.ModeSymlink != 0
		if !link && !d.Type().IsRegular() {
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
		e := newEntry(filepath.ToSlash(rel), path, info.Size())
		e.Symlink = link
		entries = append(entries, e)
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

// open returns the decompressed content of a file. A gzip stream that fails to
// decode gives an error that wraps ErrUnreadable.
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
		return nil, unreadable(err)
	}
	return readCloser{gzipReader{zr}, func() error { zr.Close(); return f.Close() }}, nil
}

func unreadable(err error) error {
	return fmt.Errorf("%w: %v", ErrUnreadable, err)
}

// gzipReader marks every decoding error as ErrUnreadable.
type gzipReader struct{ r io.Reader }

func (g gzipReader) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	if err != nil && err != io.EOF {
		err = unreadable(err)
	}
	return n, err
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

// cutLine cuts a line to MaxLineRunes.
func cutLine(line string) string {
	if len(line) <= MaxLineRunes {
		return line
	}
	return CutRunes(line, MaxLineRunes)
}

// splitLines splits decoded content into lines: a "\r" right before "\n" is
// dropped, a final "\n" doesn't start another line, and long lines are cut.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = cutLine(line)
	}
	return lines
}

// ReadLines returns every line of a file, decompressed and decoded. A file
// over MaxFileBytes gives ErrTooLarge: a plain file by its size, a gzip file
// while it is read.
func ReadLines(e Entry) ([]string, error) {
	return ReadLinesContext(context.Background(), e)
}

// ReadLinesContext accounts for decompressed bytes in this request's read phase.
func ReadLinesContext(ctx context.Context, e Entry) ([]string, error) {
	if !e.Compressed && e.Size > MaxFileBytes {
		return nil, ErrTooLarge
	}
	rc, err := open(e)
	if err != nil {
		return nil, err
	}
	return readLinesFrom(telemetry.Reader(ctx, rc))
}

// ReadLines revalidates a stored entry and opens it through an os.Root. The
// rooted open keeps the read inside the original log root if the file or one
// of its parent directories is replaced after it was first scanned.
func (r Root) ReadLines(e Entry) ([]string, error) {
	return r.ReadLinesContext(context.Background(), e)
}

// ReadLinesContext preserves rooted access while measuring the current request.
func (r Root) ReadLinesContext(ctx context.Context, e Entry) ([]string, error) {
	target, err := r.Resolve(e.Path)
	if err != nil {
		return nil, err
	}
	if target.IsDir {
		return nil, fmt.Errorf("reading %s: %w", target.Rel, fs.ErrInvalid)
	}

	root, err := os.OpenRoot(r.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(target.Rel))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("reading %s: %w", target.Rel, fs.ErrInvalid)
	}
	current := newEntry(target.Rel, target.Abs, info.Size())
	if !current.Compressed && current.Size > MaxFileBytes {
		f.Close()
		return nil, ErrTooLarge
	}
	var rc io.ReadCloser = f
	if current.Compressed {
		zr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, unreadable(err)
		}
		rc = readCloser{gzipReader{zr}, func() error { zr.Close(); return f.Close() }}
	}
	return readLinesFrom(telemetry.Reader(ctx, rc))
}

func readLinesFrom(rc io.ReadCloser) ([]string, error) {
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	return splitLines(decode(data)), nil
}

// headLines returns the first lines of a file, enough for detection.
func headLines(ctx context.Context, e Entry) ([]string, error) {
	rc, err := open(e)
	if err != nil {
		return nil, err
	}
	rc = telemetry.Reader(ctx, rc)
	defer rc.Close()
	br := bufio.NewReader(rc)
	var lines []string
	nonEmpty := 0
	for nonEmpty < detectLines {
		raw, err := readLine(br)
		if raw == nil && err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		line := cutLine(decode(raw))
		lines = append(lines, line)
		if line != "" {
			nonEmpty++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

// readLine reads one line without its "\n" or a "\r" before it, keeping at most
// the bytes MaxLineRunes runes can take and dropping the rest of the line. A
// last line without "\n" comes with io.EOF.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	cut := false
	for {
		chunk, err := br.ReadSlice('\n')
		if room := 4*MaxLineRunes - len(line); len(chunk) > room {
			chunk, cut = chunk[:room], true
		}
		line = append(line, chunk...)
		switch {
		case err == bufio.ErrBufferFull:
			continue
		case err != nil && err != io.EOF:
			return nil, err
		case err == io.EOF && len(line) == 0:
			return nil, io.EOF
		}
		if !cut {
			line = trimEnd(line)
		}
		return line, err
	}
}

// trimEnd drops a final "\n" and a "\r" right before it.
func trimEnd(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
	}
	return line
}

// DetectFile returns the format of a file from its first lines. A gzip file
// that fails to decode within those lines gives an error that wraps
// ErrUnreadable.
func DetectFile(e Entry) (string, error) {
	return DetectFileContext(context.Background(), e)
}

// DetectFileContext includes detection reads in the request's counters.
func DetectFileContext(ctx context.Context, e Entry) (string, error) {
	lines, err := headLines(ctx, e)
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
