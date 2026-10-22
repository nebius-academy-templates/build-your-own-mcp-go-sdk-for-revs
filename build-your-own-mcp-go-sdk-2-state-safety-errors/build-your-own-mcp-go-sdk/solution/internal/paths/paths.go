// Package paths screens path arguments before any file is opened.
package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Why a path is refused.
var (
	ErrOutside = errors.New("is outside the log root")
	ErrMissing = errors.New("does not exist under the log root")
)

// Error is a refused path argument, kept as the caller passed it.
type Error struct {
	Path string
	Err  error
}

func (e *Error) Error() string { return "path " + e.Path + " " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Root is a log root: the folder with every symlink resolved, and the absolute
// form it was given in, which may run through symlinks.
type Root struct {
	Dir   string
	Given string
}

// Resolve resolves a folder once, at startup.
func Resolve(dir string) (Root, error) {
	given, err := filepath.Abs(dir)
	if err != nil {
		return Root{}, err
	}
	resolved, err := filepath.EvalSymlinks(given)
	if err != nil {
		return Root{}, err
	}
	return Root{Dir: resolved, Given: given}, nil
}

// Screen turns a path argument into a location under the root and its
// relative, "/"-separated form ("." for the root). It refuses a NUL byte, an
// absolute path outside the root, a ".." that leaves the root and a symlink
// anywhere below the root with ErrOutside, and a path that isn't there with
// ErrMissing. Other errors come back as they are.
func (r Root) Screen(arg string) (abs, rel string, info fs.FileInfo, err error) {
	refuse := func(why error) (string, string, fs.FileInfo, error) {
		return "", "", nil, &Error{Path: arg, Err: why}
	}
	if strings.ContainsRune(arg, 0) {
		return refuse(ErrOutside)
	}
	switch {
	case arg == "" || arg == ".":
		rel = "."
	case filepath.IsAbs(arg):
		var ok bool
		if rel, ok = within(r.Dir, arg); !ok {
			if rel, ok = within(r.Given, arg); !ok {
				return refuse(ErrOutside)
			}
		}
	default:
		var ok bool
		if rel, ok = within(r.Dir, filepath.Join(r.Dir, arg)); !ok {
			return refuse(ErrOutside)
		}
	}
	abs = r.Dir
	if info, err = os.Lstat(abs); err != nil {
		return "", "", nil, err
	}
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			abs = filepath.Join(abs, part)
			info, err = os.Lstat(abs)
			switch {
			case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
				return refuse(ErrMissing)
			case err != nil:
				return "", "", nil, err
			case info.Mode()&fs.ModeSymlink != 0:
				return refuse(ErrOutside)
			}
		}
	}
	return abs, filepath.ToSlash(rel), info, nil
}

// within returns path relative to dir when it lies inside dir, by the path's
// text alone.
func within(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}
