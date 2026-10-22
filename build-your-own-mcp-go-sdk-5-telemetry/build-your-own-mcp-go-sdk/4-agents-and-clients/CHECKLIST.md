# Checklist: agents and clients

**Related**:

> [README](README.md): The tasks these items close
> [cases](cases.md): The questions the case items grade
> [hosts](hosts.md): The hosts the host items name

Tick each item when it is true, then commit. Where an item asks for a note, write it under the item.

## Links
**Internal**:

- `4-agents-and-clients/README.md`
- `4-agents-and-clients/CONTRACT.md`

## Server
- [ ] `cd 4-agents-and-clients/checks && go test -count=1 ./...` passes, run from the repository root.
- [ ] The practice 1 to 3 checks still pass, and so does `go -C solution test -count=1 ./...`.
- [ ] Every checkpoint in the README was true when its prompt finished. Note any that wasn't, and what you did about it.
- [ ] With `--http`, the server's stdout stays empty, and anything it logs goes to stderr.
- [ ] No token of your own is written in any file you commit. The example tokens in `tokens.example.env` and `inspector.json` are part of the practice and stay. `.mcp.json` is not in the commit.
- [ ] Nothing in `solution/` outside the `*_test.go` files and `cmd/probe/`, the prompt's text included, names a host, a date, an address or a message from `s3_logs/`.
- [ ] `solution/README.md` gives the HTTP command.

## Hosts
- [ ] Claude Code lists your server's seven tools, and six with the viewer token.
- [ ] The second host lists the seven tools. Note which host you used, and any step that differed from `hosts.md`:
- [ ] Each host asked before it ran `save_finding`.

## Cases
For each question you asked, note the symptom word and arguments you gave the prompt, and the tools the agent called.

| Question | Finding in `reports/` | Tool calls | Every part answered | Every cited line checked |
|---|---|---|---|---|
| Invoice errors (required) | | | | |
| A locked database (required) | | | | |
| A CI run full of errors (optional) | | | | |
| Failing certificate orders (optional) | | | | |

- [ ] The agent had no tools except your server's.
- [ ] Each required finding answers every part of its question, and you checked at least three of its cited lines in the files.
- [ ] Each finding says what the logs don't show, and names what happens every day as background, if anything does.
- [ ] Note any tool description or instructions change you made after watching the agent, and whether the next session went better:

## What the host showed
| Try | What the host showed | What the server could change |
|---|---|---|
| A summary of the whole log root | | |
| A search with thousands of matches | | |
| A save you refused | | |
| A save with the viewer token | | |
| The prompt with a symptom of several words | | |
| A session without a token | | |

## Inspector
- [ ] With the first example token, `tools/list` shows seven tools; with the viewer token, six.
- [ ] Without a token, the Inspector reports that the server requires authorization (HTTP 401).
- [ ] `prompts/get` for `triage` repeats a symptom of several words whole.

## Wrap-up
- [ ] Time spent on this practice, in minutes:
- [ ] `solution/`, `reports/` and this folder are committed.
