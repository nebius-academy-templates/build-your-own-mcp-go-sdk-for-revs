// Package triage builds the Log Triage MCP server and its tools.
package triage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/paths"
	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/scans"
)

const (
	serverName    = "log-triage"
	serverVersion = Version
)

// Version is the server and telemetry resource version.
const Version = "0.5.0"

// baseInstructions is the first part of the text the server publishes to
// clients (CONTRACT.md section 7).
const baseInstructions = "Log Triage reads log files under one folder. Start with list_logs to see what is there. " +
	"Use summarize_logs on a file or folder, with since and until when you know the time, to get " +
	"counts and message groups instead of raw lines. Use search_logs to find the lines that match a " +
	"pattern. Record a conclusion with save_finding and cite a file and line for every piece of " +
	"evidence. All times are UTC."

// localCaller is the one caller over stdio; it owns every scan made there.
const localCaller = "local"

// Config is what the command line decides.
type Config struct {
	Telemetry bool          // install receiving instrumentation only when export is enabled
	Root      paths.Root    // the log root, resolved once at startup
	Reports   string        // where save_finding writes
	ReadOnly  bool          // leave save_finding out
	ScanTTL   time.Duration // how long a scan_id works
	Log       *slog.Logger  // the server's own log; nil logs warnings and errors to stderr
}

// tools holds what the tool handlers share.
type tools struct {
	root  logs.Root
	cfg   Config
	scans *scans.Store[*scan]
}

// NewServer builds the MCP server with every tool of this stage registered.
func NewServer(cfg Config) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions(cfg.ScanTTL)},
	)
	log := cfg.Log
	if log == nil {
		log, _ = NewLogger(os.Stderr, DefaultLogLevel)
	}
	t := &tools{root: logs.Root{Root: cfg.Root}, cfg: cfg, scans: scans.New[*scan](cfg.ScanTTL)}
	addTool(server, log, listLogsTool(), t.listLogs)
	addTool(server, log, summarizeLogsTool(), t.summarizeLogs)
	addTool(server, log, searchLogsTool(), t.searchLogs)
	addTool(server, log, groupLinesTool(), t.groupLines)
	addTool(server, log, readLinesTool(), t.readLines)
	addTool(server, log, timelineTool(), t.timeline)
	if !cfg.ReadOnly {
		addTool(server, log, saveFindingTool(), t.saveFinding)
	}
	t.addResources(server)
	server.AddPrompt(triagePrompt(), renderTriage)
	server.AddReceivingMiddleware(freshness, callLog(log), toolsForCaller)
	if cfg.Telemetry {
		server.AddReceivingMiddleware(traceRequests)
	}
	return server
}

// toolsForCaller leaves save_finding out of a read-only caller's tools/list;
// the tool stays registered, so a call to it reaches the handler and gets E13.
// The list depends on the caller, so it is private to the caller.
func toolsForCaller(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if err != nil {
			return res, err
		}
		if list, ok := res.(*mcp.ListToolsResult); ok {
			if callerOf(ctx).ReadOnly {
				list.Tools = slices.DeleteFunc(list.Tools, func(tool *mcp.Tool) bool { return tool.Name == "save_finding" })
			}
			list.CacheScope = "private"
		}
		return res, nil
	}
}

// callLog writes one debug line per tools/call. It runs before the SDK checks
// the arguments, so a call the schema refuses gets its line too.
func callLog(log *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && method == "tools/call" {
				args := []any{"tool", params.Name, "arguments", string(params.Arguments)}
				if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
					args = append(args, "trace_id", sc.TraceID().String())
				}
				log.DebugContext(ctx, "call", args...)
			}
			return next(ctx, method, req)
		}
	}
}

// addTool registers a tool whose errors reach the caller in the contract's
// words. Any other error, and a panic, is masked: the caller gets a generic
// text and the detail goes to the log at error. That line names the tool
// without tool=, which marks the per-call lines only.
func addTool[In, Out any](server *mcp.Server, log *slog.Logger, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out Out, err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
			if err == nil {
				return
			}
			var none Out
			res, out = nil, none
			shown, known := contractError(err)
			if !known {
				log.ErrorContext(ctx, "unexpected error in "+tool.Name, "error", err.Error())
				trace.SpanFromContext(ctx).RecordError(fmt.Errorf("internal server failure"))
			}
			err = shown
		}()
		return handler(ctx, req, in)
	})
}

// instructions is the published text with the exact configured scan lifetime:
// whole minutes when the duration is an exact number of minutes, seconds otherwise.
func instructions(ttl time.Duration) string {
	lifetime := ""
	if ttl%time.Minute == 0 {
		minutes := int(ttl / time.Minute)
		lifetime = fmt.Sprintf("%d minutes", minutes)
		if minutes == 1 {
			lifetime = "1 minute"
		}
	} else {
		seconds := int(ttl / time.Second)
		lifetime = fmt.Sprintf("%d seconds", seconds)
		if seconds == 1 {
			lifetime = "1 second"
		}
	}
	return baseInstructions + " summarize_logs returns a scan_id that works for " + lifetime +
		"; pass it to group_lines to read one group's lines without scanning again. " +
		"Use read_lines for the lines around one line. Saved findings are listed at triage://reports. " +
		"Use timeline with a scan_id to see how often a pattern occurs over time, and how a number in those lines moves. " +
		"For a guided investigation, use the triage prompt."
}
