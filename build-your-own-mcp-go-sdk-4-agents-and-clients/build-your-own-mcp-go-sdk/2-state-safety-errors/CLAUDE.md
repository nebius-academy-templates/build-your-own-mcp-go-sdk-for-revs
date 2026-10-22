# Rules for this practice: state, safety and errors

**Related**:

> [README](README.md): The tasks and their prompts
> [CONTRACT](CONTRACT.md): The exact surface to build
> [hostile-inputs](hostile-inputs.md): Inputs the server must refuse
> [log-formats](../docs/log-formats.md): How to read every log line

These rules take priority over the repository root's `CLAUDE.md` wherever the two differ. `AGENTS.md` in this folder is an identical copy.

## What this practice builds
The second version of the Log Triage server, in `solution/`, on top of what practice 1 built, with go-sdk v1.7.0 and jsonschema-go v0.4.3 on Go 1.26:

- version `0.2.0`, the longer instructions and the `--scan-ttl` flag;
- a scan store: `summarize_logs` returns a `scan_id`, and the store keeps each scan's groups for later calls;
- two tools: `group_lines` and `read_lines`;
- four resources: `triage://formats`, `triage://reports`, `triage://reports/{name}`, `triage://scans/{scan_id}`, with the freshness pair;
- path screening, symlink handling, the size and line caps, unreadable archives;
- the contract's error texts, the checks behind `save_finding`'s schema, and masking of unexpected errors.

`CONTRACT.md` in this folder describes the whole server after this practice, practice 1 included. Build one prompt at a time, only what that prompt asks for.

## What not to build yet
Later practices add these. Don't add them now, even where they look useful:

- a caller other than `local`: tokens, principals from requests, per-caller tool lists;
- tests of your own under `solution/`, and programs that call the server;
- a `timeline` tool, prompts, or any tool, resource or template not in `CONTRACT.md`;
- HTTP, a `--log-level` flag, or any flag not in `CONTRACT.md`;
- a cache of anything other than scans, or a scan store that outlives the process.

## Files you may change
| Path | When |
|---|---|
| `solution/` | always; all server code and its `go.mod`, `go.sum` and `README.md` |
| `2-state-safety-errors/interface.md`, `2-state-safety-errors/CHECKLIST.md` | only when the student asks you to |
| `reports/` | only through the server's `save_finding` tool |

Never change this folder's `CONTRACT.md`, `README.md`, `CLAUDE.md`, `AGENTS.md`, `hostile-inputs.md`, `inspector.json` or anything in `checks/`, and never write into `s3_logs/`, `docs/` or `1-surface-and-contract/`. If the contract and the checks seem to disagree, say so instead of changing either.

## How to work
- Look up go-sdk v1.7.0 and jsonschema-go v0.4.3 behaviour in their documentation before you rely on it; don't guess an API. The freshness pair and masking are not server options there: the pair is set in a receiving middleware, and masking is a wrapper around the tool handlers.
- A resource handler that can't find what was asked returns the protocol's resource-not-found error itself, with the code and message in `CONTRACT.md`, section 4. go-sdk's own `mcp.ResourceNotFoundError` carries the URI only in its data, not in its message.
- A receiving middleware gets a nil result next to an error: return at once when the error isn't nil. go-sdk doesn't recover a panic in a handler, so the process ends; the masking wrapper recovers panics too.
- The error texts in `CONTRACT.md`, section 5, are exact. Build each one in one place and quote values with plain `"` characters, not with `%q`.
- Screen path arguments where they are built, walk folders without following symlinks, and revalidate cached file paths before reopening them. Reopened files must stay confined to the original root even if a file or parent directory was replaced after a scan. The log root is resolved once at startup; only what lies below it is screened.
- Keep practice 1 working: its checks in `1-surface-and-contract/checks/` must still pass after every prompt.
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
| Run this practice's checks | `go -C 2-state-safety-errors/checks test -count=1 ./...` |
| Run one task's checks | `go -C 2-state-safety-errors/checks test -count=1 harness_test.go t3_group_lines_test.go` |
| Run practice 1's checks | `go -C 1-surface-and-contract/checks test -count=1 ./...` |

The checks build the server themselves with `go build -C solution` into a temporary folder and start it with their own `--root`, `--reports` and `--scan-ttl`. `checks/` is its own Go module: don't import the server from it, and don't add a `go.work` file. There is one check file per task, `t2_scans_test.go` to `t7_checks_test.go`; after the prompt for task K, the files for tasks 2 to K pass, the later ones still fail, and practice 1's checks pass. Add `-count=1` to every `go test ./...` a prompt names: the checks build the server in another process, so without it Go can answer from its test cache after `solution/` changed.
