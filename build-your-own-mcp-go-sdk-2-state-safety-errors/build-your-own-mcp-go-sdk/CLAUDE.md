# Rules for any coding agent in this repository

**Related**:

> [README](README.md): What this repository builds and how the practices run
> [log-formats](docs/log-formats.md): The only source of log format knowledge
> [s3-logs](docs/s3-logs.md): What the log bundle holds

This repository is a course: a student and their coding agent build Log Triage, an MCP server for investigating logs, one practice folder at a time. `AGENTS.md` is an identical copy of this file for agents that read that name.

## Rules
1. **The practice folder's rules come first.** Before the first task of a practice, read that practice folder's `CLAUDE.md` (or its identical `AGENTS.md`). Where it and this file differ, follow the practice folder.
2. **Server code goes only in `solution/`.** In a practice folder, change only the files that folder's rules say you may edit.
3. **Never write into `s3_logs/`.** It's the log bundle every practice and check reads. Code that needs its own log files creates them in a temporary folder.
4. **The practice's `CONTRACT.md` is exact.** Tool, parameter and field names, types, bounds, defaults, annotations, texts and the server's `instructions` are taken as written. The checks compare them.
5. **Log formats come from `docs/log-formats.md`.** Parse by its rules, and nothing else.
6. **The server knows formats, not incidents.** No string in the server's source names a host, a date, an address, an incident or a message from `s3_logs/`, and no code ranks, labels or filters a message by what it says beyond the format rules. The agent that uses the server draws every conclusion. Test code may name what it tests.
7. **stdout belongs to the protocol.** Over stdio, the server writes nothing to stdout except MCP messages. Diagnostics go to stderr.
8. **Investigate through the server.** When you're asked what happened in the logs, use the Log Triage server's tools. Don't read, search or decompress files in `s3_logs/` directly.
9. **Don't build ahead.** Build what the current prompt asks for. A practice folder's `CLAUDE.md` names what its stage leaves out.
10. **Commit when a task is done**, with the message the practice's README gives.

## Commands
Run from the repository root unless a row says otherwise.

| What | Command |
|---|---|
| Build the server | `go -C solution build -o bin/log-triage ./cmd/log-triage` |
| Run the server over stdio | `solution/bin/log-triage --root s3_logs` |
| Run a practice's checks | `go -C <practice folder>/checks test -count=1 ./...` |

The server's command line is in the practice's `CONTRACT.md`. The checks build and start your server themselves; you don't need to build or run it first. Each practice's `checks/` folder is its own Go module that runs your server as a program: don't import one module from the other, and don't add a `go.work` file. Each practice's `README.md` shows how to open the MCP Inspector on it.

## Writing
Plain prose in comments, READMEs and findings. Cite a file and a line for every claim about the logs.
