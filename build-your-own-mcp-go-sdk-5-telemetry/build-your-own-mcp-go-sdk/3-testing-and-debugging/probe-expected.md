# Probe transcript

**Related**:

> [README](README.md): Project guide and probe command
> [CONTRACT](CONTRACT.md): The error texts the transcript shows

What the probe does and what it prints. The probe is a small scripted client: it starts your server over stdio, as a host would, sends five requests in an order a careless host might, and prints one line per request. The checks for task 4 run it and compare its lines with this page.

## Links
**Internal**:

- `3-testing-and-debugging/CONTRACT.md`
- `3-testing-and-debugging/README.md`

## How the probe runs
- It takes the log root with `--root DIR` and starts the server itself, with that root, `--scan-ttl 1` and a temporary `--reports` folder.
- It talks MCP to the server through the SDK's client, in the current protocol revision.
- It prints the five step lines below on stdout, in this order, and may print other lines around them.
- It exits with code 0 when every step got the answer this page names, and with code 1 otherwise.

The practice `README.md` and `CLAUDE.md` give the command that runs it.

## The steps
| Step | Name | Request | Expected answer |
|---|---|---|---|
| 1 | `out-of-order` | `group_lines` with a well-formed `scan_id` that no scan made, and any well-formed `group_id` | a tool error with E6 for that id |
| 2 | `scan` | `summarize_logs` on one file of your choice | a result with a `scan_id` |
| 3 | `expired` | `group_lines` with the `scan_id` of step 2, sent at least two seconds later | a tool error with E6 for that id |
| 4 | `unknown-argument` | `list_logs` with an argument the tool doesn't have | a refusal before the tool runs |
| 5 | `hostile-path` | `read_lines` with `path` `../outside.log` and `line` 1 | a tool error with E1 for that path |

A refusal before the tool runs is whatever the SDK answers for a schema violation: a JSON-RPC error, or a result with `isError: true` and no `structuredContent`. The probe reports its kind, never its wording, because SDKs word it differently.

## The line format
Each step prints exactly one line:

```text
step <k> <name>: <tool> -> <outcome>
```

`<outcome>` is one of:

- `ok scan_id=<scan_id>` for a result that carries a scan;
- `ok` for any other result;
- `tool error: <text>` for a result with `isError: true` whose text is one of the contract's error texts (`CONTRACT.md`, section 5, E1 to E12, placeholders filled), with that text exactly as the server sent it;
- `refused` for a JSON-RPC error, or for any other result with `isError: true`.

A contract error and an SDK refusal can look alike: both may be a result with `isError: true` and no `structuredContent`. The text decides, so compare it with the contract's texts, not the result's shape.

A transcript that matches this page, with made-up ids:

```text
step 1 out-of-order: group_lines -> tool error: scan "s_0000000000000000" is unknown or has expired. Call summarize_logs again for a new scan_id.
step 2 scan: summarize_logs -> ok scan_id=s_5caf26916f306b2e
step 3 expired: group_lines -> tool error: scan "s_5caf26916f306b2e" is unknown or has expired. Call summarize_logs again for a new scan_id.
step 4 unknown-argument: list_logs -> refused
step 5 hostile-path: read_lines -> tool error: path "../outside.log" is outside the log root. Pass a path relative to the log root; list_logs with path "." shows what is there.
```

## What the transcript tells you
- Step 1: a host that calls tools out of order gets told what to do next, not a crash.
- Steps 2 and 3: a handle expires at its published time, and the error says how to get a new one.
- Step 4: the schema refuses an argument before your code sees it.
- Step 5: a path that climbs out of the log root is refused before any file is opened.
