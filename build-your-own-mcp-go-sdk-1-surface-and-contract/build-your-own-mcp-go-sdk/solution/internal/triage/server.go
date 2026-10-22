// Package triage builds the Log Triage MCP server and its tools.
package triage

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

const (
	serverName    = "log-triage"
	serverVersion = "0.1.0"
)

// instructions is the text the server publishes to clients (CONTRACT.md section 5).
const instructions = "Log Triage reads log files under one folder. Start with list_logs to see what is there. " +
	"Use summarize_logs on a file or folder, with since and until when you know the time, to get " +
	"counts and message groups instead of raw lines. Use search_logs to find the lines that match a " +
	"pattern. Record a conclusion with save_finding and cite a file and line for every piece of " +
	"evidence. All times are UTC."

// Config is what the command line decides.
type Config struct {
	Root     string // the log root, an absolute path to a readable folder
	Reports  string // where save_finding writes
	ReadOnly bool   // leave save_finding out
}

// tools holds what the tool handlers share.
type tools struct {
	root logs.Root
	cfg  Config
}

// NewServer builds the MCP server with every tool of this stage registered.
func NewServer(cfg Config) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions},
	)
	t := &tools{root: logs.Root{Dir: cfg.Root}, cfg: cfg}
	mcp.AddTool(server, listLogsTool(), t.listLogs)
	mcp.AddTool(server, summarizeLogsTool(), t.summarizeLogs)
	mcp.AddTool(server, searchLogsTool(), t.searchLogs)
	if !cfg.ReadOnly {
		mcp.AddTool(server, saveFindingTool(), t.saveFinding)
	}
	return server
}
