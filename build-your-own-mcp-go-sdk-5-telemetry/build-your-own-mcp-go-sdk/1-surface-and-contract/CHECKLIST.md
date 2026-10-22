# Checklist: the surface and the contract

**Related**:

> [README](README.md): The tasks these items close
> [interface](interface.md): Your original design and notes about differences from the contract

Tick each item when it is true, then commit. Where an item asks for a note, write it under the item.

## Links
**Internal**:

- `1-surface-and-contract/README.md`
- `1-surface-and-contract/CONTRACT.md`

## Design
- [ ] Every row of `interface.md` is filled in.
- [ ] `interface.md` names where your design differs from `CONTRACT.md`, or says it doesn't.

## Server
- [ ] `cd 1-surface-and-contract/checks && go test -count=1 ./...` passes, run from the repository root.
- [ ] Every checkpoint in the README was true when its prompt finished. Note any that wasn't, and what you did about it.
- [ ] `solution/` contains no host, date, address or message copied from `s3_logs/`.
- [ ] `solution/README.md` says how to build and run your server.

## Inspector
- [ ] `tools/list` on `log-triage` shows four tools, each with the title, annotations, input schema and output schema that `CONTRACT.md` gives.
- [ ] `tools/list` on `log-triage-read-only` shows the same tools without `save_finding`.
- [ ] The `summarize_logs` call returns `structuredContent` and one text block with the same object.
- [ ] What the Inspector shows matches `CONTRACT.md`; `interface.md` keeps your original choices and explains any differences.
- [ ] Every description says what its tool does, when to use it and how it differs from a nearby tool where that choice could be unclear.

## Case
- [ ] The agent used only your server's tools to solve the case; the transcript shows no other tool calls.
- [ ] The finding in `reports/` explains why the customer got an error page, and when.
- [ ] Every piece of evidence names a file and a line, and you checked that at least three of those lines say what the finding claims.
- [ ] Note which tools the agent called, and anything you changed after watching it.

## Wrap-up
- [ ] Time spent on this practice, in minutes:
- [ ] `solution/`, `reports/` and this folder are committed.
