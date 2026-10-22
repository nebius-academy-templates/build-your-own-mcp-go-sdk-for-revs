# Checklist: state, safety and errors

**Related**:

> [README](README.md): The tasks these items close
> [interface](interface.md): Your state design, which the Inspector items compare against
> [hostile-inputs](hostile-inputs.md): The inputs the safety items try

Tick each item when it is true, then commit. Where an item asks for a note, write it under the item.

## Links
**Internal**:

- `2-state-safety-errors/README.md`
- `2-state-safety-errors/CONTRACT.md`

## Design
- [ ] Every row of `interface.md` is filled in with your own answers, written before you opened `CONTRACT.md`.
- [ ] `interface.md` names where your design differs from `CONTRACT.md`, or says it doesn't.

## Server
- [ ] `cd 2-state-safety-errors/checks && go test -count=1 ./...` passes, run from the repository root.
- [ ] `cd 1-surface-and-contract/checks && go test -count=1 ./...` still passes.
- [ ] Every checkpoint in the README was true when its prompt finished. Note any that wasn't, and what you did about it.
- [ ] Nothing in `solution/` names a host, a date, an address or a message from `s3_logs/`.
- [ ] `solution/README.md` names `--scan-ttl`.

## Inspector
- [ ] `resources/list` shows `triage://formats` and `triage://reports`, and `resources/templates/list` shows the two templates, each with its MIME type.
- [ ] `triage://reports` reads as a document whose `count` matches the findings saved in `reports/`.
- [ ] A read of `triage://reports/no-such-finding` answers the resource-not-found error, and its message or data holds the URI.
- [ ] The `ttlMs` and `cacheScope` of a resource list or read, seen in the Inspector's Protocol tab or in the raw-frame check of `t5_resources_test.go`. Note where you saw them.
- [ ] On `log-triage-expiry`, a `scan_id` works with `group_lines` right away and answers E6 after five seconds.
- [ ] What the Inspector shows matches the state section of your `interface.md`. Note what surprised you.

## Safety
- [ ] Every input in `hostile-inputs.md` got the answer its table gives.
- [ ] No error text you saw shows where the log root is on disk, an exception name or a stack trace.

## Case
- [ ] The agent had no tools except your server's.
- [ ] Note whether the agent paged a group of a scan with `group_lines`, and if it didn't, what it did instead.
- [ ] The finding in `reports/` explains why the deploys failed and when, and says whether the SSH activity is related.
- [ ] Every piece of evidence names a file and a line, and you checked that at least three of those lines say what the finding claims.
- [ ] Note which tools the agent called, and anything you changed after watching it.

## Wrap-up
- [ ] Time spent on this practice, in minutes:
- [ ] `solution/`, `reports/` and this folder are committed.
