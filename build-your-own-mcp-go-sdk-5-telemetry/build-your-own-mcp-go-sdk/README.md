# Build your own MCP server: Log Triage (Go, go-sdk)

**Start here**:

> [CLAUDE](CLAUDE.md): The rules your coding agent follows in this repository
> [log-formats](docs/log-formats.md): How to read every log format in the bundle
> [s3-logs](docs/s3-logs.md): What the log bundle holds

You build Log Triage, an MCP server that lets an AI agent investigate a bundle of logs on your machine. You give the server a folder of logs. The agent asks it which files are there, what went wrong in a file or a folder over some window of time, and which lines match a pattern, and gets back counts, message groups and a few lines instead of whole files. The server knows how to read log formats and nothing else: every conclusion is the agent's.

You write the whole server yourself, with your AI agent, in `solution/`. Each practice folder adds one stage. Its `README.md` explains that stage's surface, project layout, setup, checks, and completion criteria. The lesson supplies the build prompts and investigation questions.

## Links
- [MCP Go SDK documentation](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk@v1.7.0/mcp)
- [jsonschema-go documentation](https://pkg.go.dev/github.com/google/jsonschema-go@v0.4.3/jsonschema)
- [MCP specification](https://modelcontextprotocol.io/specification/2026-07-28)
- [MCP Inspector CLI](https://modelcontextprotocol.io/docs/2026-07-28/tools/inspector/cli)
- [Go documentation](https://go.dev/doc/)

**Internal**:

- [CLAUDE.md](CLAUDE.md)
- [docs/log-formats.md](docs/log-formats.md)
- [docs/s3-logs.md](docs/s3-logs.md)
- [solution/README.md](solution/README.md)

## Practices
Work through them in order. Each one continues the server the previous one left.

| Practice | Folder | What you build |
|---|---|---|
| 1 | [1-surface-and-contract](1-surface-and-contract/README.md) | The server, its log reader, and four tools to list, summarize and search the logs and to save a finding |
| 2 | [2-state-safety-errors](2-state-safety-errors/README.md) | Scan handles, two tools to page a group's lines and read around a line, resources, path screening, exact error texts and masking |
| 3 | [3-testing-and-debugging](3-testing-and-debugging/README.md) | Your own unit, protocol and meaning tests, a scripted probe, planted bugs, logging, and a timeline tool |
| 4 | [4-agents-and-clients](4-agents-and-clients/README.md) | HTTP with bearer tokens, callers and a read-only caller, a triage prompt, and the server in two agent hosts |
| 5 | [5-telemetry](5-telemetry/README.md) | OTLP tracing, caller context, per-file read spans, and a traced investigation |

## Prerequisites
| Tool | Version | Used for |
|---|---|---|
| Go | 1.27.1 or newer | the server and the checks |
| go-sdk | v1.7.0 (`github.com/modelcontextprotocol/go-sdk`), pinned by the server's `go.mod` | the MCP server library |
| jsonschema-go | v0.4.3 (`github.com/google/jsonschema-go`), pinned by the server's `go.mod` | the tools' input and output schemas |
| Node.js | 22.19 or newer | the MCP Inspector, through `npx` |
| An agent host | Claude Code or another coding agent that can use MCP servers | building the server and asking the questions |
| A second agent host | Codex CLI or another host that can use MCP servers over HTTP | practice 4 |
| git | any recent version | committing each practice |
| LangSmith account | the free Developer plan | the trace backend in practice 5, beside a supplied local viewer |

## How to start
1. Clone this repository and open it in your agent host at the repository root.
2. Read `CLAUDE.md`. Your agent reads it too, and so does every practice folder's own `CLAUDE.md`.
3. Open the first practice in the table above and follow its `README.md` from the top.

## Layout
```text
README.md             this file
CLAUDE.md             rules for any coding agent; a practice folder's rules take priority
AGENTS.md             the same rules, for agents that read AGENTS.md
.gitignore
docs/
  log-formats.md      the nine log formats: sample lines, time, level, message, grouping
  s3-logs.md          what the log bundle holds: layout, calendar, sizes
s3_logs/              the log bundle; read-only
reports/              where your server saves findings; commit them
solution/               your MCP server; it starts with only a README
```

Practice folders appear here as you reach them. Each holds its own `README.md`, `CLAUDE.md`, `AGENTS.md`, `CONTRACT.md`, `CHECKLIST.md`, `inspector.json`, a `checks/` suite and the stage's worksheets.

## Commit a practice
When a practice's checks pass and its checklist is done, commit the server, your findings and your worksheets together:

```bash
git add solution reports <practice folder>
git commit -m "Practice <N>: <what you built>"
```

Then go to the next practice. The server keeps growing in `solution/`; the practice folders hold only what each stage needs. Build output in `solution/bin/` is ignored.
