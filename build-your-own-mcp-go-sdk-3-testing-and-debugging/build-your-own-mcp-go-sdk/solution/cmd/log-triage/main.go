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
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/triage"
)

func main() {
	flags := flag.NewFlagSet("log-triage", flag.ContinueOnError)
	root := flags.String("root", "", "the log root; every path argument is resolved under it (required)")
	reports := flags.String("reports", "reports", "where save_finding writes; created if missing")
	readOnly := flags.Bool("read-only", false, "do not register save_finding")
	scanTTL := flags.Int("scan-ttl", 900, "how many seconds a scan_id works, 1 or more")
	logLevel := flags.String("log-level", triage.DefaultLogLevel,
		"the server's own log threshold, one of "+strings.Join(triage.LogLevelNames(), ", ")+"; the log goes to stderr")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *scanTTL < 1 {
		fmt.Fprintf(os.Stderr, "--scan-ttl must be 1 or more seconds, got %d\n", *scanTTL)
		os.Exit(2)
	}
	log, err := triage.NewLogger(os.Stderr, *logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	logRoot, ok := readableFolder(*root)
	if !ok {
		fmt.Fprintf(os.Stderr, "log root \"%s\" is not a readable folder\n", *root)
		os.Exit(2)
	}
	absReports, err := filepath.Abs(*reports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reports folder %q: %v\n", *reports, err)
		os.Exit(2)
	}

	server := triage.NewServer(triage.Config{
		Root:     logRoot,
		Reports:  absReports,
		ReadOnly: *readOnly,
		ScanTTL:  time.Duration(*scanTTL) * time.Second,
		Log:      log,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "log-triage:", err)
		os.Exit(1)
	}
}

// readableFolder resolves dir when it is a folder the server can list.
func readableFolder(dir string) (paths.Root, bool) {
	if dir == "" {
		return paths.Root{}, false
	}
	root, err := paths.Resolve(dir)
	if err != nil {
		return paths.Root{}, false
	}
	f, err := os.Open(root.Dir)
	if err != nil {
		return paths.Root{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return paths.Root{}, false
	}
	if _, err := f.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return paths.Root{}, false
	}
	return root, true
}
