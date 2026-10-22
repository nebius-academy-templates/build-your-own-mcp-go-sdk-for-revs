# Log Triage contract

**Related**:

> [README](README.md): This practice: tasks, prompts, checks
> [log-formats](../docs/log-formats.md): How every log line is read; part of this contract

What your server publishes after this practice. Every name, type, bound, default, annotation, output field and text below is exact: the checks in `checks/` compare them. Tool descriptions are yours to write; only their length is checked.

## Links
- [MCP specification: Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)

**Internal**:

- `docs/log-formats.md`
- `1-surface-and-contract/checks/`

## 1. Server
| Item | Value |
|---|---|
| Name | `log-triage` |
| Version | `0.1.0` |
| Python | package `logtriage`, console script `log-triage`, Python 3.13, `fastmcp==4.0.3` |
| Go | module `github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server`, command `cmd/log-triage`, Go 1.26, `github.com/modelcontextprotocol/go-sdk v1.7.0` |
| Transport | stdio |
| `instructions` | the text in section 5 |

### Command line
| Flag | Meaning | Default |
|---|---|---|
| `--root DIR` | The log root. Every `path` argument is resolved under it. Required | none |
| `--reports DIR` | Where `save_finding` writes. Created if missing | `reports` in the working directory |
| `--read-only` | Do not register `save_finding` | off |

A missing or unreadable `--root` makes the server exit with code 2 and print one line on stderr: `log root "<DIR>" is not a readable folder`, with `<DIR>` as given on the command line.

Nothing the server prints goes to stdout except the protocol.

## 2. Reading logs
`docs/log-formats.md` is part of this contract. Format detection, the nine formats, untimed lines, times and message groups are specified there.

### Paths
- A `path` argument is a string relative to the log root, or an absolute path inside it. `.` or an empty string means the root.
- Output paths are always relative to the root, `/`-separated, with no leading `./`. The root itself is `.`.
- A folder means every regular file below it, recursively, in byte order of the relative path. Files and folders whose names start with `.` are skipped.
- Paths are cleaned and checked lexically. A path outside the root is refused (section 4). This practice assumes trusted log and report directories; it does not reject symlinks inside them.

### Files
- A name ending in `.gz` is gzip; it is decompressed as a stream. Everything else is read as it is.
- Content is decoded as UTF-8 with invalid bytes replaced. `\r` before `\n` is dropped. Line numbers are 1-based in the decoded content.
- Every length and cut in this contract (1,000, 200, 500 and the parameter limits) counts Unicode code points; in Go, runes of the decoded content.
- A file of format `unknown` is listed by `list_logs` and skipped by `summarize_logs` and `search_logs` with reason `unknown format`.
- `skipped` entries are `{"path": str, "reason": str}`, at most 20 listed, sorted by path.

### Times
- Output: `YYYY-MM-DDTHH:MM:SSZ`, UTC, fractions cut.
- Input `since` and `until`: `YYYY-MM-DD` (midnight UTC), or `YYYY-MM-DDTHH:MM` with optional `:SS`, an optional fraction after the seconds, and an optional `Z` or `±HH:MM`. No zone means UTC.
- Every time is cut to whole seconds when it is read: line times, run headers, `since` and `until`. Windows, `first_seen`, `last_seen` and the group order compare the cut values.
- A window is `since <= time < until`. Either end may be omitted. With neither end given, every line is in the window, lines of files that have no time included. With either end given, a line without a time is outside it.
- Timestamps are trusted as written. No file is skipped because of the date in its name.

## 3. Tools
### Output rules
- Every tool publishes an `outputSchema` and returns `structuredContent` that validates against it, plus one text block holding the same object as JSON. The text is compared parsed, not as bytes, so key order is free. A successful result has `isError` absent or `false`.
- Parameter types are exactly as the tables give them. A parameter written "or null" publishes a type list with `null` (`["null", "string"]`) or an `anyOf` with a `null` branch, and its `default: null` may be present or absent. Any other parameter publishes its plain type and nothing else. In Go, a pointer or a slice where a table says a plain type publishes a nullable type and fails the checks; go-sdk fills schema defaults itself, so `limit` and `offset` need no pointer.
- Parameters with a default are not in `required`.
- Lists in results are `[]` when empty, never `null`. An output schema may still type a list or an object as nullable; the value is what is checked.
- A paged or capped list comes with `total` (all that exist), `shown` (in this answer) and `note`.
  - A tool with `offset`: `note` is `""` when no later page exists (`offset + shown >= total`, which includes an `offset` past the end with `shown` 0). Otherwise it is `Showing <shown> of <total> <things>. Pass offset=<next> for more.`, with `<next>` = `offset + shown` and `<things>` = `files` or `matches`.
  - `summarize_logs`: `note` is `""` when every group is shown. Otherwise it is `Showing <shown> of <total> groups. Raise limit (up to 100) or narrow the path or the window.`
- Line text in any output is cut to 500 characters. No output field holds a log line except `example.text` in `summarize_logs` and `text` in `search_logs`.

### Titles and annotations
| Tool | `title` | `readOnlyHint` | `destructiveHint` | `idempotentHint` | `openWorldHint` |
|---|---|---|---|---|---|
| `list_logs` | List log files | true | | | false |
| `summarize_logs` | Summarize logs | true | | | false |
| `search_logs` | Search logs | true | | | false |
| `save_finding` | Save a finding | false | true | false | false |

- The title may sit on the tool, in its annotations, or both; wherever it appears, it is the table's text.
- Read-only tools set `readOnlyHint` and `openWorldHint` and leave `destructiveHint` out, because the specification ignores it when `readOnlyHint` is true.
- An absent `readOnlyHint` or `idempotentHint` means `false`, and the checks treat the two alike. go-sdk v1.7.0 always writes both, so a Go server shows `idempotentHint: false` on its read-only tools.
- `destructiveHint` and `openWorldHint` default to `true`, so every `false` in the table is written out. `save_finding` is destructive because another call with the same title overwrites the earlier report.
- Every tool has a description of 40 to 400 characters.

### list_logs
What files are under a path, and what format each one is.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `path` | string | max 500 chars | `"."` |
| `limit` | integer | 1 to 500 | 50 |
| `offset` | integer | 0 or more | 0 |

Output: `path` (normalized, `.` for the root), `files` (list of `path`, `size_bytes` on disk, `compressed` boolean, `format`), `total`, `shown`, `note`. A file path is allowed and lists that one file. `compressed` is true for a `.gz` name.

### summarize_logs
Counts and message groups for a file or folder in a window. The tool that answers "what went wrong here" without reading every line.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `path` | string | max 500 chars | `"."` |
| `since` | string or null | max 40 chars | null |
| `until` | string or null | max 40 chars | null |
| `min_level` | enum `any`, `debug`, `info`, `warning`, `error`, `critical` | | `warning` |
| `limit` | integer | 1 to 100 | 20 |

Output:

| Field | Type | Meaning |
|---|---|---|
| `path`, `since`, `until`, `min_level` | | The request, normalized; times in the output format, `null` when not given |
| `files_scanned` | integer | Files read, skipped ones excluded |
| `skipped` | list | See section 2 |
| `lines_scanned` | integer | Every line read |
| `lines_in_window` | integer | Lines whose time is in the window (section 2, Times) |
| `first_seen`, `last_seen` | string or null | The earliest and latest time over the lines in the window |
| `by_level` | object | `critical`, `error`, `warning`, `info`, `debug`, `unknown` counts over the lines in the window, all six keys always present |
| `groups` | list | Groups at or above `min_level` (`any` includes `unknown`), in the group order of `docs/log-formats.md`, at most `limit` |
| `total_groups` | integer | The groups at or above `min_level`, not every group in the window |
| `shown`, `note` | | See Output rules |

A group: `group_id`, `format`, `level`, `pattern`, `count`, `first_seen`, `last_seen` (the earliest and latest time of its lines, `null` when they have none), `file_count`, `files` (the first 5 paths in byte order), `example` (`file`, `line` and `text` of the first line of the group in scan order; `text` is the raw line, cut to 500 characters, not the message).

`min_level` order, low to high: `unknown` (only reached through `any`), `debug`, `info`, `warning`, `error`, `critical`.

### search_logs
Lines that match a regular expression.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `path` | string | max 500 chars | `"."` |
| `pattern` | string | 1 to 200 chars; the common subset of Python `re` and Go RE2, case-sensitive, `(?i)` allowed | required |
| `since`, `until` | string or null | max 40 chars each | null |
| `min_level` | enum as in `summarize_logs` | | `any` |
| `limit` | integer | 1 to 200 | 50 |
| `offset` | integer | 0 or more | 0 |

The pattern is searched anywhere in the raw line, not only in the message. A line matches when its time is in the window, its level is at or above `min_level`, and the pattern is found. Output: `path`, `pattern`, `matches` (list of `file`, `line`, `time` or null, `level`, `text`), `total` (every match), `shown`, `note`. Order: file path, then line.

### save_finding
Writes one finding as a markdown file. The only write.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `title` | string | 3 to 120 chars | required |
| `severity` | enum `low`, `medium`, `high`, `critical` | | required |
| `summary` | string | 1 to 4,000 chars | required |
| `evidence` | list of `file` (string, max 500), `line` (integer, 1 or more), `note` (string, 1 to 300 chars); all three required in every item | 1 to 20 items | required |

The file name is the slug of the title plus `.md`. The slug, in this order:

1. Lowercase the ASCII letters.
2. Every run of characters other than `a` to `z` and `0` to `9` becomes one `-`.
3. Trim `-` at both ends.
4. Cut to 60 characters.
5. Trim a trailing `-` again.
6. An empty slug becomes `finding`.

The file name is joined to `--reports`; another call with the same slug overwrites that file. The trusted-directory assumption in section 2 applies. Its content:

```markdown
# <title>

- Severity: <severity>
- Saved: <UTC time>

<summary>

## Evidence

- `<file>` line <line>: <note>
```

The file is exactly this block: one evidence line per item, in the given order, and a single `\n` at the end. `Saved` is the time in the output format.

Output: `name` (the slug), `path` (relative to `--reports`), `created` (the same value as `Saved`).

With `--read-only`, `save_finding` is not registered, and every other tool stays.

## 4. Errors
- **Invalid arguments.** A value out of bounds, a value outside an enum, an unknown argument or a missing required argument is refused by the SDK as a result with `isError: true` and no `structuredContent`. The checks do not inspect the wording.
- **Unknown tool.** A call to a tool that is not registered is refused as a JSON-RPC error.
- **Refused by your tool.** A `path` outside the log root comes back as a result with `isError: true`. The wording is yours.

## 5. Instructions
At most 512 characters, exactly:

> Log Triage reads log files under one folder. Start with list_logs to see what is there. Use summarize_logs on a file or folder, with since and until when you know the time, to get counts and message groups instead of raw lines. Use search_logs to find the lines that match a pattern. Record a conclusion with save_finding and cite a file and line for every piece of evidence. All times are UTC.
