# Rules for this practice: the surface and the contract

**Related**:

> [README](README.md): The tasks and their prompts
> [CONTRACT](CONTRACT.md): The exact surface to build
> [log-formats](../docs/log-formats.md): How to read every log line

These rules take priority over the repository root's `CLAUDE.md` wherever the two differ. `AGENTS.md` in this folder is an identical copy.

## What this practice builds
The first version of the Log Triage server, in `solution/`, with go-sdk v1.7.0 and jsonschema-go v0.4.3 on Go 1.26:

- the server object, its name, version and instructions, its three command-line flags, stdio;
- a log reader that follows `docs/log-formats.md`;
- four tools: `list_logs`, `summarize_logs`, `search_logs` and `save_finding`, exactly as `CONTRACT.md` describes them.

Build one prompt at a time, only what that prompt asks for. Each prompt names the files it needs; use those names.

## What not to build yet
Later practices add these. Don't add them now, even where they look useful:

- a `scan_id` or any other handle, or a cache of scan results; every call reads the files again;
- tools other than the four above;
- resources, resource templates and prompts;
- error texts beyond refusing a path outside the log root; the refusal's wording is yours;
- checks behind the schema (evidence files and lines, existing finding names), masking of unexpected errors, size or line caps, symlink handling;
- HTTP, tokens, a `--log-level` flag or any other flag;
- tests of your own under `solution/`.

## Files you may change
| Path | When |
|---|---|
| `solution/` | always; all server code and its `go.mod`, `go.sum` and `README.md` |
| `1-surface-and-contract/interface.md`, `1-surface-and-contract/CHECKLIST.md` | only when the student asks you to |
| `reports/` | only through the server's `save_finding` tool |

Never change this folder's `CONTRACT.md`, `README.md`, `CLAUDE.md`, `AGENTS.md`, `inspector.json` or anything in `checks/`, and never write into `s3_logs/` or `docs/`. If the contract and the checks seem to disagree, say so instead of changing either.

## How to work
- Look up go-sdk v1.7.0 and jsonschema-go v0.4.3 behaviour in their documentation before you rely on it; don't guess an API.
- Follow `docs/log-formats.md` rule by rule. Count lengths and cuts in runes, not bytes.
- The server knows log formats and nothing about what happened in `s3_logs/`: no host, date, address, incident or message from the logs appears in `solution/`.
- Over stdio, stdout belongs to the protocol. Print diagnostics to stderr only.
- When the student asks what happened in the logs, answer through the Log Triage tools, not by reading `s3_logs/` directly.

## Commands
Run from the repository root.

| What | Command |
|---|---|
| Update the server's module requirements | `go -C solution mod tidy` |
| Build the server | `go -C solution build -o bin/log-triage ./cmd/log-triage` |
| Run the server over stdio | `solution/bin/log-triage --root s3_logs` |
| Run this practice's checks | `go -C 1-surface-and-contract/checks test -count=1 ./...` |
| Run one task's checks | `go -C 1-surface-and-contract/checks test -count=1 harness_test.go t4_list_logs_test.go` |

The checks build the server themselves with `go build -C solution` into a temporary folder and start it with their own `--root` and `--reports`. `checks/` is its own Go module: don't import the server from it, and don't add a `go.work` file. There is one check file per task, `t2_server_test.go` to `t7_save_finding_test.go`; after the prompt for task K, the files for tasks 2 to K pass and the later ones still fail.
