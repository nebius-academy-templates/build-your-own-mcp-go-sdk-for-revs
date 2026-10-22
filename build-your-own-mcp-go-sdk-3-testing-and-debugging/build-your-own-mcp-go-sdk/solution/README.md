# Log Triage server

An MCP server that reads log files under one folder and answers over stdio. It is one Go module, `github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server`, built with go-sdk v1.7.0 on Go 1.26.

## Links
- [MCP Go SDK documentation](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk@v1.7.0/mcp)

**Internal**:

- `1-surface-and-contract/CONTRACT.md`
- `docs/log-formats.md`

## Build
From the repository root:

```bash
go -C solution mod tidy
go -C solution build -o bin/log-triage ./cmd/log-triage
```

The binary lands in `solution/bin/`, which git ignores.

## Run
From the repository root:

```bash
solution/bin/log-triage --root s3_logs
```

The server speaks MCP on stdin and stdout, so it waits for a client. Stop it with Ctrl+C.

| Flag | Meaning | Default |
|---|---|---|
| `--root DIR` | The log root; every `path` argument is resolved under it. Required | none |
| `--reports DIR` | Where `save_finding` writes; created if missing | `reports` in the working directory |
| `--read-only` | Leave `save_finding` out | off |
| `--scan-ttl SECONDS` | How long a `scan_id` from `summarize_logs` works; 1 or more | `900` |
| `--log-level LEVEL` | `debug`, `info`, `warning` or `error`; log lines go to stderr, and `debug` writes one `tool=<name>` line per call | `warning` |

A missing or unreadable `--root`, a `--scan-ttl` below 1 and an unknown `--log-level` exit with code 2 and one line on stderr.

## Test
From the repository root:

```bash
go -C solution test -count=1 ./...
```

The unit tests for the log reader and the grouping sit next to the code in `internal/logs/`. `-count=1` runs them every time instead of reporting a cached result.

## Probe
A scripted client that starts the server over stdio, sends five requests in a careless order and prints one line per request (`3-testing-and-debugging/probe-expected.md`). From the repository root:

```bash
go -C solution run ./cmd/probe --root ../s3_logs
```

`go -C solution` runs it from `solution/`, so `--root` is relative to that folder. It exits 0 when every request got the expected answer.

## Layout
```text
cmd/log-triage/main.go       flags, stdio
internal/triage/server.go    the server object
```
