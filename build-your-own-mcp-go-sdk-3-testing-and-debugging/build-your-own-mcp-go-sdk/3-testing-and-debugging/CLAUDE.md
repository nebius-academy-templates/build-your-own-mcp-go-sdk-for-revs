# Rules for this practice: testing and debugging

**Related**:

> [README](README.md): Stage overview, setup and verification
> [CONTRACT](CONTRACT.md): The exact surface to build
> [tricky-cases](tricky-cases.md): The unit test cases
> [mutations](mutations.md): The planted bugs of task 5
> [probe-expected](probe-expected.md): The probe's steps and output

These rules take priority over the repository root's `CLAUDE.md` wherever the two differ. `AGENTS.md` in this folder is an identical copy.

## What this practice builds
The third version of the Log Triage server, in `solution/`, on top of what practices 1 and 2 built, with go-sdk v1.7.0 and jsonschema-go v0.4.3 on Go 1.26:

- the server's own tests, next to the code: `internal/logs/formats_test.go` and `groups_test.go` (unit), and in `internal/triage/` `protocol_test.go`, `flow_test.go` and `facts_test.go` (protocol, flow, meaning), with shared helpers in `setup_test.go`;
- a scripted client in `solution/cmd/probe/`;
- the `--log-level` flag, logging to stderr with `log/slog` in `internal/triage/logging.go`, one line per tool call at `debug`;
- the `timeline` tool with E11 and E12, version `0.3.0` and the longer instructions.

`CONTRACT.md` in this folder describes the whole server after this practice. Build one prompt at a time, only what that prompt asks for. Each prompt and this folder's documents name the required files; use those names.

## Out of scope
Do not add these in this stage, even where they look useful:

- HTTP, tokens, or a caller other than `local`;
- prompts, including a `triage` prompt;
- any tool, resource, flag or error text not in `CONTRACT.md`;
- tests that start the server as a process: the protocol and flow tests use go-sdk's in-memory transports, and only the probe starts a process.

## Files you may change
| Path | When |
|---|---|
| `solution/` | always; all server code, tests, the probe, `go.mod`, `go.sum` and `README.md` |
| `3-testing-and-debugging/CHECKLIST.md` | only when the student asks you to |
| `reports/` | only through the server's `save_finding` tool |

Never change this folder's `CONTRACT.md`, `README.md`, `CLAUDE.md`, `AGENTS.md`, `tricky-cases.md`, `mutations.md`, `facts.json`, `probe-expected.md`, `inspector.json` or anything in `checks/`, and never write into `s3_logs/`, `docs/` or the earlier practice folders. If the contract and the checks seem to disagree, say so instead of changing either.

## How to work
- Look up go-sdk v1.7.0 and Go's `testing` and `log/slog` behaviour in their documentation before you rely on it; don't guess an API. `mcp.NewInMemoryTransports()` gives a connected pair for a server and a client in the test process; connect the server first.
- In the current protocol revision, go-sdk's client keeps `resources/read` and list results for their `ttlMs` (30 seconds here). A test that reads the same resource again after a change needs a new client session.
- Go runs each test with its package folder as the working directory: reach `s3_logs/` and `facts.json` with paths relative to it, such as `../../../s3_logs` from `internal/triage/`.
- Tests may name files in `s3_logs/` and compare with `facts.json`, but no test states what went wrong in the logs.
- When a test fails, decide first whether the code or the test is wrong, and say which you changed.
- Only stderr lines containing `tool=` are counted, so other stderr lines are fine. Nothing may reach stdout except the protocol.
- go-sdk checks a call's arguments against the tool's schema before it runs the handler you pass to `mcp.AddTool`, so a handler never sees a refused call. The `tool=` line has to come from a receiving middleware (`Server.AddReceivingMiddleware`), which sees every `tools/call`.
- A mutation from `mutations.md` changes the server code only. Before one, the student commits; to undo one, put the code back with `git restore -- ':(glob)solution/**/*.go' ':(glob,exclude)solution/**/*_test.go'`, which leaves the test files as they are.
- Keep practices 1 and 2 working: their checks must still pass after every prompt. Run every `go test` with `-count=1`: the checks build the server in another process, so without it Go can answer from its test cache after `solution/` changed.
- Follow `docs/log-formats.md` rule by rule. Count lengths and cuts in runes, not bytes.
- The server knows log formats and nothing about what happened in `s3_logs/`: no host, date, address, incident or message from the logs appears in the server's non-test code.
- When the student asks what happened in the logs, answer through the Log Triage tools, not by reading `s3_logs/` directly.

## Commands
Run from the repository root.

| What | Command |
|---|---|
| Build the server | `go -C solution build -o bin/log-triage ./cmd/log-triage` |
| Run the server over stdio | `solution/bin/log-triage --root s3_logs` |
| Run the server's own tests | `go -C solution test -count=1 ./...` |
| Run one package of them | `go -C solution test -count=1 ./internal/logs/` |
| Run the probe | `go -C solution run ./cmd/probe --root ../s3_logs` |
| Undo a mutation | `git restore -- ':(glob)solution/**/*.go' ':(glob,exclude)solution/**/*_test.go'` |
| Run this practice's checks | `cd 3-testing-and-debugging/checks && go test -count=1 ./...` |
| Run one task's checks | `cd 3-testing-and-debugging/checks && go test -count=1 harness_test.go t4_probe_test.go` |
| Run the earlier practices' checks | `cd 2-state-safety-errors/checks && go test -count=1 ./...`, `cd 1-surface-and-contract/checks && go test -count=1 ./...` |

The checks build the server themselves with `go build -C solution` into a temporary folder, run your own tests with `go test -count=1 -json ./...` in `solution/`, and run the probe as above. They sort your tests by file: the `*_test.go` files in `internal/logs/` are unit tests, and files named `protocol*_test.go`, `flow*_test.go` and `facts*_test.go` hold the protocol, flow and meaning tests. `checks/` is its own Go module: don't import the server from it, and don't add a `go.work` file. There is one check file per build task: `t1_unit_test.go`, `t2_protocol_test.go`, `t3_meaning_test.go`, `t4_probe_test.go`, `t6_logging_test.go` and `t7_timeline_test.go`. Task 5, the mutations, has none. After the prompt for task K, the files for the tasks up to K pass, the later ones still fail, and the practice 1 and 2 checks pass.
