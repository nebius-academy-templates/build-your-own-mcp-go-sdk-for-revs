# Checklist: testing and debugging

**Related**:

> [README](README.md): Stage overview and verification commands
> [mutations](mutations.md): The mutations the table below records

Tick each item when it is true, then commit. Where an item asks for a note, write it under the item.

## Links
**Internal**:

- `3-testing-and-debugging/README.md`
- `3-testing-and-debugging/CONTRACT.md`

## Your tests
- [ ] `go -C solution test -count=1 ./...` passes, with at least 10 tests across the unit, protocol, flow and meaning files.
- [ ] Each layer tests something the others don't. Note one thing only your meaning tests would catch.
- [ ] Note which meaning test failed on its first run and what you fixed, or why none did and why you trust them anyway.

## Mutations
| Mutation | The test that failed | Its message, in short |
|---|---|---|
| 1, an inverted window | | |
| 2, a wrong total | | |
| 3, state that leaks between scans | | |

- [ ] Every row names a test. If you had to add one, note which.
- [ ] After the last undo, your tests pass again.

## Server
- [ ] `cd 3-testing-and-debugging/checks && go test -count=1 ./...` passes, run from the repository root.
- [ ] The practice 1 and 2 checks still pass.
- [ ] Every task's Verify step was true before you continued. Note any that wasn't, and what you did about it.
- [ ] No production Go file under `solution/` names a host, a date, an address or a message from `s3_logs/`.
- [ ] `solution/README.md` gives the test, probe and `--log-level` commands.

## Debugging
- [ ] The probe prints the five steps of `probe-expected.md`, and you can say what each one shows.
- [ ] At `--log-level debug`, the server writes one `tool=` line per call to stderr, and stdout carries only the protocol.
- [ ] Your diagnostic trace for the `vms/db-01` data question: the reproducing `summarize_logs` call, schema verdict, expected and actual source-line evidence, direct reader result on those lines, and the layer that diverged or why the data explains the answer:

## Inspector
- [ ] `tools/list` shows `timeline` with its title, annotations and schemas.
- [ ] In the web UI, `timeline` on a scan shows empty buckets, per-bucket values, and E11 for a `value_pattern` without a group.
- [ ] The probe's lines and what the Inspector showed agree.

## Case
- [ ] The agent used only your server's MCP tools for the case; the transcript shows no other tool calls.
- [ ] The finding in `reports/` shows the day-by-day trend with numbers, says what ended it and when, and says whether error counts showed it.
- [ ] Every piece of evidence names a file and a line, and you checked that at least three of those lines say what the finding claims.
- [ ] Note which tools the agent called, and anything you changed after watching it.

## Wrap-up
- [ ] Time spent on this practice, in minutes:
- [ ] `solution/`, `reports/` and this folder are committed.
