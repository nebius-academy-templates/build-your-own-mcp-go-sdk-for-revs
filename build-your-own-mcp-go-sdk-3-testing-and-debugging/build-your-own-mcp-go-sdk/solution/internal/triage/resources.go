package triage

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	formatsURI     = "triage://formats"
	reportsURI     = "triage://reports"
	reportTemplate = "triage://reports/{name}"
	scanTemplate   = "triage://scans/{scan_id}"
	reportPrefix   = "triage://reports/"
	scanPrefix     = "triage://scans/"

	jsonType     = "application/json"
	markdownType = "text/markdown"
)

// Freshness hints on every resource list and read (CONTRACT.md section 4).
const (
	resourceTTLMs      = 30_000
	resourceCacheScope = "private"
)

// formatInfo is one entry of triage://formats.
type formatInfo struct {
	Name      string   `json:"name"`
	Levels    []string `json:"levels"`
	Timestamp string   `json:"timestamp"`
	Example   string   `json:"example"`
}

// formats lists the nine formats in the order of docs/log-formats.md, each with
// the levels its rules can give, its timestamp shape and its first sample line
// that has the format's shape.
var formats = []formatInfo{
	{"app", []string{"critical", "error", "warning", "info", "debug", "unknown"}, "yyyy-mm-ddTHH:MM:SS.fffZ",
		"2026-01-15T10:04:12.345Z ERROR billing charge failed order=A-1042 amount=19.99 client=192.0.2.10 trace=3f2a9c1e7b6d4a58"},
	{"nginx-access", []string{"error", "warning", "info", "unknown"}, "dd/Mon/yyyy:HH:MM:SS ±zzzz",
		`192.0.2.10 - - [15/Jan/2026:10:04:12 +0100] "GET /catalog/items?page=2 HTTP/1.1" 502 157 "-" "curl/8.5.0"`},
	{"nginx-error", []string{"critical", "error", "warning", "info", "debug", "unknown"}, "yyyy/mm/dd HH:MM:SS",
		`2026/01/15 10:04:12 [crit] 41#41: *7 open() "/var/www/myhost/cache/a1.tmp" failed (13: Permission denied), client: 192.0.2.10, server: myhost`},
	{"php-fpm", []string{"critical", "error", "warning", "info", "debug", "unknown"}, "dd-Mon-yyyy HH:MM:SS",
		"[15-Jan-2026 10:04:12] WARNING: [pool www] child 38 exited on signal 9 (SIGKILL) after 12.5 seconds from start"},
	{"postgres", []string{"critical", "error", "warning", "info", "debug", "unknown"}, "yyyy-mm-dd HH:MM:SS.fff UTC",
		`2026-01-15 10:04:12.345 UTC [2231] ERROR:  duplicate key value violates unique constraint "users_email_key"`},
	{"redis", []string{"warning", "info", "debug", "unknown"}, "dd Mon yyyy HH:MM:SS.fff",
		"1:M 15 Jan 2026 10:04:12.345 # WARNING overcommit_memory is set to 0! Background save may fail under low memory condition."},
	{"syslog", []string{"unknown"}, "yyyy-mm-ddTHH:MM:SS.fff±hh:mm",
		"2026-01-15T10:04:12.345678+02:00 myhost sshd[812]: Accepted publickey for deploy from 192.0.2.10 port 50122 ssh2"},
	{"pytest", []string{"error", "info", "unknown"}, "run header", "=== run 2026-01-15T10:00:00Z ==="},
	{"gotest", []string{"error", "info", "debug", "unknown"}, "run header", "=== run 2026-01-15T10:00:00Z ==="},
}

// reportInfo is one entry of triage://reports.
type reportInfo struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Created  string `json:"created"`
}

// addResources registers the triage:// resources and templates.
func (t *tools) addResources(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		Name: "formats", URI: formatsURI, MIMEType: jsonType,
		Description: "The nine log formats the server reads: levels, timestamp shape and a sample line.",
	}, t.readFormats)
	server.AddResource(&mcp.Resource{
		Name: "reports", URI: reportsURI, MIMEType: jsonType,
		Description: "The findings saved with save_finding: name, title, severity and when each was saved.",
	}, t.readReports)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "report", URITemplate: reportTemplate, MIMEType: markdownType,
		Description: "One saved finding, as the markdown file save_finding wrote.",
	}, t.readReport)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "scan", URITemplate: scanTemplate, MIMEType: jsonType,
		Description: "The summarize_logs answer of a scan that hasn't expired.",
	}, t.readScan)
}

func (t *tools) readFormats(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return jsonContents(req.Params.URI, map[string]any{"formats": formats})
}

func (t *tools) readReports(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	reports, err := t.reports()
	if err != nil {
		return nil, err
	}
	return jsonContents(req.Params.URI, map[string]any{"count": len(reports), "reports": reports})
}

func (t *tools) readReport(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	name := strings.TrimPrefix(uri, reportPrefix)
	reports, err := t.reports()
	if err != nil {
		return nil, err
	}
	i := sort.Search(len(reports), func(i int) bool { return reports[i].Name >= name })
	if i == len(reports) || reports[i].Name != name {
		return nil, resourceNotFound(uri)
	}
	data, err := os.ReadFile(filepath.Join(t.cfg.Reports, name+".md"))
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: markdownType, Text: string(data)}}}, nil
}

func (t *tools) readScan(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	kept, found := t.scans.Get(localCaller, strings.TrimPrefix(uri, scanPrefix))
	if !found {
		return nil, resourceNotFound(uri)
	}
	return jsonContents(uri, kept.summary)
}

// reports lists the saved findings: regular *.md files directly under the
// reports folder, dot files and symlinks left out, that have the finding's
// title, severity and saved lines. They come sorted by name.
func (t *tools) reports() ([]reportInfo, error) {
	entries, err := os.ReadDir(t.cfg.Reports)
	if errors.Is(err, fs.ErrNotExist) {
		return []reportInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	reports := []reportInfo{}
	for _, e := range entries {
		name, isMarkdown := strings.CutSuffix(e.Name(), ".md")
		if !isMarkdown || strings.HasPrefix(e.Name(), ".") || strings.Contains(name, ".") || !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(t.cfg.Reports, e.Name()))
		if err != nil {
			return nil, err
		}
		if info, ok := findingInfo(name, string(data)); ok {
			reports = append(reports, info)
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	return reports, nil
}

// findingInfo reads the title, severity and saved lines of a finding file.
func findingInfo(name, content string) (reportInfo, bool) {
	info := reportInfo{Name: name}
	var title, severity, created bool
	for _, line := range strings.Split(content, "\n") {
		switch {
		case !title && strings.HasPrefix(line, "# "):
			info.Title, title = strings.TrimPrefix(line, "# "), true
		case !severity && strings.HasPrefix(line, "- Severity: "):
			info.Severity, severity = strings.TrimPrefix(line, "- Severity: "), true
		case !created && strings.HasPrefix(line, "- Saved: "):
			info.Created, created = strings.TrimPrefix(line, "- Saved: "), true
		}
	}
	return info, title && severity && created
}

func jsonContents(uri string, value any) (*mcp.ReadResourceResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: jsonType, Text: string(data)}}}, nil
}

// resourceNotFound is the protocol's resource-not-found error, with the URI in
// its message and its data.
func resourceNotFound(uri string) error {
	data, _ := json.Marshal(map[string]string{"uri": uri})
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Resource not found: " + uri, Data: data}
}

// freshness sets the contract's cache hints on resource lists and reads. The
// SDK fills its own defaults after a resource handler runs, so the hints are
// set on the way out.
func freshness(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if err != nil {
			return res, err
		}
		switch r := res.(type) {
		case *mcp.ListResourcesResult:
			r.TTLMs, r.CacheScope = resourceTTLMs, resourceCacheScope
		case *mcp.ListResourceTemplatesResult:
			r.TTLMs, r.CacheScope = resourceTTLMs, resourceCacheScope
		case *mcp.ReadResourceResult:
			r.TTLMs, r.CacheScope = resourceTTLMs, resourceCacheScope
		}
		return res, nil
	}
}
