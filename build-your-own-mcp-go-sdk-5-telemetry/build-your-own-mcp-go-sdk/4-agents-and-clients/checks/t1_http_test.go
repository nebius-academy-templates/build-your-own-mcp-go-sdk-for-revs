package checks

// Task 1: --http, the tokens, 401, the Origin check, both protocol revisions, an empty stdout.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

const task1 = "Task 1 adds --http with bearer tokens and the Origin check."

var m1 = module("t1")

func TestHTTPWithoutTokensExits2(t *testing.T) {
	running := ownHTTPServer(t, nil)
	code := running.exitCode()
	if code == -1 {
		t.Fatalf("with --http and no LOG_TRIAGE_TOKENS the server must exit, but it started. %s", task1)
	}
	if code != 2 {
		t.Fatalf("with --http and no LOG_TRIAGE_TOKENS the exit code must be 2, got %d", code)
	}
	if !slices.Contains(strings.Split(running.stderr.String(), "\n"), tokensMissing) {
		t.Fatalf("stderr must hold the line %q (CONTRACT.md, section 1); last lines:\n%s",
			tokensMissing, tail(running.stderr.String(), 15))
	}
}

func TestToolsListWithAToken(t *testing.T) {
	s := m1.server(t)
	tools, _ := s.rpc(t, "tools/list", nil, owner, "", nil).result(t)["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		if m, isObject := tool.(map[string]any); isObject {
			name, _ := m["name"].(string)
			names[name] = true
		}
	}
	var missing []string
	for _, want := range expected.Tools.Writer {
		if !names[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tools/list over HTTP with a known token lacks %v. %s", missing, task1)
	}
}

func TestAToolCallWithAToken(t *testing.T) {
	checkAnswer(t, m1.server(t), "summary-app-window", owner, owner)
}

func TestNoKnownTokenGets401(t *testing.T) {
	for _, c := range []struct{ id, header string }{
		{"no-header", ""},
		{"basic", "Basic YWJjOmRlZg=="},
		{"empty-bearer", "Bearer"},
		{"unknown-token", "Bearer " + unknownToken},
	} {
		t.Run(c.id, func(t *testing.T) {
			var headers map[string]string
			if c.header != "" {
				headers = map[string]string{"Authorization": c.header}
			}
			bearerChallenge(t, m1.server(t).rpc(t, "tools/list", nil, "", "", headers))
		})
	}
}

func TestAForeignOriginGets403(t *testing.T) {
	for _, origin := range expected.Origins.Forbidden {
		t.Run(origin, func(t *testing.T) {
			for _, c := range []struct{ id, token string }{{"with-token", owner}, {"without-token", ""}} {
				t.Run(c.id, func(t *testing.T) {
					s := m1.server(t)
					sent := fill(origin, map[string]string{"port": fmt.Sprint(s.port)}).(string)
					raw := s.rpc(t, "tools/list", nil, c.token, sent, nil)
					if raw.status != 403 {
						t.Fatalf("%s; an Origin other than http://localhost[:port] or http://127.0.0.1[:port] must answer "+
							"403, before the token is looked at (CONTRACT.md, section 7). %s", raw.describe(), task1)
					}
				})
			}
		})
	}
}

func TestALocalOriginPasses(t *testing.T) {
	for _, origin := range expected.Origins.Allowed {
		t.Run(origin, func(t *testing.T) {
			s := m1.server(t)
			sent := fill(origin, map[string]string{"port": fmt.Sprint(s.port)}).(string)
			s.rpc(t, "tools/list", nil, owner, sent, nil).result(t)
		})
	}
}

func TestTheOlderProtocolRevision(t *testing.T) {
	s := m1.server(t)
	hello := s.post(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": legacy, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "log-triage-checks", "version": "4"}},
	}, owner, "", nil, "initialize "+legacy)
	if version, _ := hello.result(t)["protocolVersion"].(string); version == "" {
		t.Fatalf("%s; initialize must answer a protocolVersion", hello.describe())
	}
	listed := s.post(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		owner, "", map[string]string{"MCP-Protocol-Version": legacy}, "tools/list "+legacy+", no session id")
	tools, _ := listed.result(t)["tools"].([]any)
	found := false
	for _, tool := range tools {
		if m, isObject := tool.(map[string]any); isObject && m["name"] == "list_logs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s; after an initialize, a tools/list without a session id must be answered "+
			"(stateless HTTP, CONTRACT.md section 7)", listed.describe())
	}
}

func TestStdoutStaysEmpty(t *testing.T) {
	s := m1.server(t)
	s.rpc(t, "tools/list", nil, owner, "", nil).result(t)
	s.rpc(t, "tools/list", nil, "", "", nil)
	s.call(t, "list_logs", map[string]any{"path": "pods"}, owner)
	if out := s.stdout.String(); out != "" {
		t.Fatalf("with --http nothing may reach stdout, the HTTP server's access lines included; stdout holds:\n%q",
			cut(out, 300))
	}
}

func TestOneToolLinePerCallOverHTTP(t *testing.T) {
	running := ownHTTPServer(t, withTokens(), "--log-level", "debug")
	running.call(t, "list_logs", map[string]any{"path": "pods"}, owner)
	running.call(t, "read_lines", map[string]any{"path": "pods/provisioner-2/archive/app-2026-08-30.log.gz", "line": 1}, owner)
	running.call(t, "list_logs", map[string]any{"path": "pods", "no_such_argument": 1}, owner)
	var lines []string
	for _, line := range strings.Split(running.stderr.String(), "\n") {
		if strings.Contains(line, "tool=") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("three tools/call requests over HTTP at --log-level debug must write three lines containing tool=, "+
			"one of them for the call the schema refused (CONTRACT.md, section 1); got %d:\n%s",
			len(lines), strings.Join(lines[:min(len(lines), 10)], "\n"))
	}
	if running.stdout.String() != "" {
		t.Fatal("with --http nothing may reach stdout, logging included")
	}
}
