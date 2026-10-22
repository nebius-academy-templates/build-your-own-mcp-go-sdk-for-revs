# Mutations

**Related**:

> [README](README.md): Project guide, test commands and mutation workflow
> [CHECKLIST](CHECKLIST.md): Where you record which test caught each one

Three small bugs to put into your server on purpose. Each one keeps the server running and its answers well formed, so only a test that checks what the answer means can catch it. Apply them one at a time, run your own tests, write down which test failed, and put the server back.

## Links
**Internal**:

- `3-testing-and-debugging/README.md`
- `3-testing-and-debugging/facts.json`

## How to run each mutation
1. Commit your server, or stash your changes, so the mutation can be undone safely.
2. Send the mutation's first prompt to your agent. It changes the server and runs your own tests.
3. Note in `CHECKLIST.md` which of your tests failed, with its message.
4. If none failed, your suite has a gap: ask your agent for the test that would catch the mutation, check that it fails now, and keep it.
5. Send the mutation's second prompt. It puts the server back as it was committed, and your tests pass again.

The practice `CLAUDE.md` names the commands for running your tests and for putting the server back.

## Mutation 1: an inverted window
The window check lets in a line stamped exactly at `until` and leaves out a line stamped exactly at `since`. A time window still looks right at a glance, and the counts move only by the lines on the two edges.

A test catches it when it compares a window's count with a known one: every file in `facts.json` has a window whose two ends fall on its own lines, with how many lines sit on each end.

#### Prompt
```text
Mutation 1 of practice 3 (see 3-testing-and-debugging/mutations.md): in the server's window
check, make until inclusive and since exclusive. Change nothing else, and don't touch the
tests. Then run the server's own tests and tell me which tests fail and why.
```

#### Prompt
```text
Undo mutation 1: put the server code back as it was committed, as the practice CLAUDE.md says.
Then run the server's own tests and confirm that they all pass.
```

## Mutation 2: a wrong total
`search_logs` reports as `total` the number of matches on the page it returns, not every match in the window. The first page of a small search still looks right.

A test catches it when it searches with a `limit` smaller than the number of matches and checks `total` and the paging note.

#### Prompt
```text
Mutation 2 of practice 3 (see 3-testing-and-debugging/mutations.md): make search_logs report
as total the number of matches on the returned page instead of every match. Change nothing
else, and don't touch the tests. Then run the server's own tests and tell me which tests fail
and why.
```

#### Prompt
```text
Undo mutation 2: put the server code back as it was committed, as the practice CLAUDE.md says.
Then run the server's own tests and confirm that they all pass.
```

## Mutation 3: state that leaks between scans
`summarize_logs` keeps its counting state between calls, so every call adds its counts to the ones before it. The first call after a start is still right.

A test catches it when it calls `summarize_logs` twice with the same arguments on one running server and expects the same counts both times.

#### Prompt
```text
Mutation 3 of practice 3 (see 3-testing-and-debugging/mutations.md): make summarize_logs keep
its level counts and group tallies in module-level state that is never cleared, so each call
adds to the previous ones. Change nothing else, and don't touch the tests. Then run the
server's own tests and tell me which tests fail and why.
```

#### Prompt
```text
Undo mutation 3: put the server code back as it was committed, as the practice CLAUDE.md says.
Then run the server's own tests and confirm that they all pass.
```
