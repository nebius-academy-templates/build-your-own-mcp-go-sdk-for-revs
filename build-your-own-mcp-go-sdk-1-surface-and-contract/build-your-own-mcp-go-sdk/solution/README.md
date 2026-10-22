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

A missing or unreadable `--root` exits with code 2 and one line on stderr.

## Layout
```text
cmd/log-triage/main.go       flags, stdio
internal/triage/server.go    the server object
```
