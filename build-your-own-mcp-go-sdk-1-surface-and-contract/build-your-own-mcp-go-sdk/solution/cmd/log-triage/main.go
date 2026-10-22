// Command log-triage serves the Log Triage MCP server over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/triage"
)

func main() {
	flags := flag.NewFlagSet("log-triage", flag.ContinueOnError)
	root := flags.String("root", "", "the log root; every path argument is resolved under it (required)")
	reports := flags.String("reports", "reports", "where save_finding writes; created if missing")
	readOnly := flags.Bool("read-only", false, "do not register save_finding")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	absRoot, ok := readableFolder(*root)
	if !ok {
		fmt.Fprintf(os.Stderr, "log root \"%s\" is not a readable folder\n", *root)
		os.Exit(2)
	}
	absReports, err := filepath.Abs(*reports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reports folder %q: %v\n", *reports, err)
		os.Exit(2)
	}

	server := triage.NewServer(triage.Config{Root: absRoot, Reports: absReports, ReadOnly: *readOnly})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "log-triage:", err)
		os.Exit(1)
	}
}

// readableFolder returns the absolute path of dir when it is a folder the
// server can list.
func readableFolder(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return "", false
	}
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return "", false
	}
	return abs, true
}
