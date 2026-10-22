# Rules for this practice: agents and clients

**Related**:

> [README](README.md): The tasks and their prompts
> [CONTRACT](CONTRACT.md): The exact surface to build
> [hosts](hosts.md): How agent hosts connect over HTTP
> [cases](cases.md): The questions asked through a host

These rules take priority over the repository root's `CLAUDE.md` wherever the two differ. `AGENTS.md` in this folder is an identical copy.

## What this practice builds
The fourth and last version of the Log Triage server, in `solution/`, on top of what practices 1 to 3 built, with go-sdk v1.7.0 on Go 1.26:

- `--http HOST:PORT` in `cmd/log-triage/main.go`: stateless streamable HTTP at `/mcp`, set up in `internal/httpserve/httpserve.go`, with nothing on stdout;
- callers from the bearer tokens in `LOG_TRIAGE_TOKENS`, the 401 answer and the `Origin` check, in `internal/httpserve/httpserve.go`, and the caller carried in the request context by `internal/triage/principal.go`;
- scans owned by the caller, and a read-only caller that doesn't see `save_finding` and gets E13 when it calls it;
- the `triage` prompt in `internal/triage/prompts.go`, the prompt sentence of the instructions and version `0.4.0`.

Over stdio nothing changes: it stays the default, with the one caller `local`. After the build, the student registers the server in two agent hosts (`hosts.md`), asks the questions of `cases.md` through them and saves the findings in `reports/`.

`CONTRACT.md` in this folder describes the whole server after this practice. Build one prompt at a time, only what that prompt asks for. The project structure in this folder's `README.md` names the files; use those names.

## What not to build
This is the last practice. Don't add, even where it looks useful:

- OAuth, token issuing, or any HTTP route other than `/mcp`;
- callers from anywhere but `LOG_TRIAGE_TOKENS`, or tokens written to a file;
- sessions, or anything kept per connection: a `scan_id` is the only thing that carries from one request to the next;
- any tool, resource, prompt, flag or error text not in `CONTRACT.md`.

## Files you may change
| Path | When |
|---|---|
| `solution/` | always; all server code, its tests, `go.mod`, `go.sum` and `README.md` |
| `4-agents-and-clients/CHECKLIST.md` | only when the student asks you to |
| `reports/` | only through the server's `save_finding` tool |
| `.mcp.json` at the repository root | only through `claude mcp add --scope project`, when the student asks you to |

Never change this folder's `CONTRACT.md`, `README.md`, `CLAUDE.md`, `AGENTS.md`, `hosts.md`, `cases.md`, `tokens.example.env`, `inspector.json` or anything in `checks/`, and never write into `s3_logs/`, `docs/` or the earlier practice folders. Never write a token into any file; the example tokens stay in `tokens.example.env`. If the contract and the checks seem to disagree, say so instead of changing either.

## How to work
- Look up go-sdk v1.7.0 in its documentation before you rely on it: `mcp.NewStreamableHTTPHandler` and `StreamableHTTPOptions.Stateless`, `Server.AddReceivingMiddleware`, `Server.AddPrompt` and `RequestExtra`. Don't guess an API.
- There is one scan store for the whole process. The streamable HTTP handler calls its `getServer` function on every request; return the one `*mcp.Server` built at startup. A server or a store built inside that function loses every scan.
- The caller travels in the request's context: the bearer check puts it there, and the context reaches the tool and resource handlers. Over stdio nothing puts it there, so the caller is `local`.
- Write the bearer check and the `Origin` check as your own `net/http` middleware, the `Origin` check first. go-sdk's `auth.RequireBearerToken` answers 401 without a `WWW-Authenticate` header unless it is given resource metadata or required scopes. `http.CrossOriginProtection` trusts only exact origins, so it can't allow any port, and it lets every `GET` through.
- Hiding `save_finding` from a read-only caller and answering E13 are two separate pieces of code. Keep the tool registered unless `--read-only`; a receiving middleware removes it from the `tools/list` result for a read-only caller, and its handler answers E13. A tool that isn't registered gets go-sdk's unknown-tool refusal, not E13. go-sdk checks the arguments against the schema before the handler runs, so E13 comes only with valid arguments.
- go-sdk marks list results `cacheScope` `"public"`. The tool list depends on the caller, so set `"private"` on `tools/list`.
- go-sdk's own guard answers 403 to a request whose `Host` header isn't local; leave it on.
- Tests of your own for HTTP go in `internal/httpserve/httpserve_test.go`: `httptest.NewServer` with the handler from `internal/httpserve`, and go-sdk's `StreamableClientTransport` with an `http.Client` that adds the `Authorization` header. A hand-written request in protocol revision 2026-07-28 also needs the `Mcp-Method` header (and `Mcp-Name` on `tools/call`, `prompts/get` and `resources/read`); go-sdk's client sends them itself. The protocol and flow tests of practice 3 keep using the in-memory transports.
- Keep practices 1 to 3 working, your own tests included: their checks run over stdio after every prompt. Run every `go test` with `-count=1`: the checks build the server in another process, so without it Go can answer from its test cache after `solution/` changed.
- The `triage` prompt's text is instructions for an agent. Like everything in `solution/`, it names no host, date, address, incident or message from `s3_logs/`.
- go-sdk doesn't check a prompt's required arguments: the `triage` handler itself refuses a `prompts/get` without `symptom`, with a JSON-RPC error of code -32602 (`&jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, …}`).
- Over stdio, stdout belongs to the protocol. With `--http`, stdout stays empty. Print diagnostics to stderr only.
- When the student asks what happened in the logs, answer through the Log Triage tools, not by reading `s3_logs/` directly.

## Commands
Run from the repository root.

| What | Command |
|---|---|
| Build the server | `go -C solution build -o bin/log-triage ./cmd/log-triage` |
| Run the server over stdio | `solution/bin/log-triage --root s3_logs` |
| Load the example tokens | `set -a; . 4-agents-and-clients/tokens.example.env; set +a` |
| Run the server over HTTP | `solution/bin/log-triage --root s3_logs --reports reports --http 127.0.0.1:8765` |
| Run the server's own tests | `go -C solution test -count=1 ./...` |
| Run this practice's checks | `cd 4-agents-and-clients/checks && go test -count=1 ./...` |
| Run one task's checks | `cd 4-agents-and-clients/checks && go test -count=1 harness_test.go t1_http_test.go` |
| Run the earlier practices' checks | `cd 3-testing-and-debugging/checks && go test -count=1 ./...`, and the same in `2-state-safety-errors/checks` and `1-surface-and-contract/checks` |

The checks build the server themselves with `go build -C solution` into a temporary folder, and start it with their own `--http 127.0.0.1:<free port>` and their own `LOG_TRIAGE_TOKENS`. `checks/` is its own Go module: don't import the server from it, and don't add a `go.work` file. There is one check file per build task: `t1_http_test.go`, `t2_principals_test.go` and `t3_prompt_test.go`. Tasks 4 to 7 happen in agent hosts and the MCP Inspector, and `CHECKLIST.md` records them. After the prompt for task K, the files for the tasks up to K pass, the later ones still fail, and the practice 1 to 3 checks pass.
