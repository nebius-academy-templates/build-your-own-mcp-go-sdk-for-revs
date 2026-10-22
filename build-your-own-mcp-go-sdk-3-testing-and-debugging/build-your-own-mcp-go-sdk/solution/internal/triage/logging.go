package triage

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// logLevels maps the --log-level values to slog levels, lowest first.
var logLevels = []struct {
	name  string
	level slog.Level
}{
	{"debug", slog.LevelDebug},
	{"info", slog.LevelInfo},
	{"warning", slog.LevelWarn},
	{"error", slog.LevelError},
}

// DefaultLogLevel is the --log-level the server starts with.
const DefaultLogLevel = "warning"

// LogLevelNames lists the --log-level values, lowest first.
func LogLevelNames() []string {
	names := make([]string, len(logLevels))
	for i, l := range logLevels {
		names[i] = l.name
	}
	return names
}

// NewLogger returns a logger that writes lines at level and above to w, which
// is stderr for the server: stdout carries only the protocol.
func NewLogger(w io.Writer, level string) (*slog.Logger, error) {
	for _, l := range logLevels {
		if l.name == level {
			return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l.level})), nil
		}
	}
	return nil, fmt.Errorf("--log-level must be one of %s, got %q", strings.Join(LogLevelNames(), ", "), level)
}
