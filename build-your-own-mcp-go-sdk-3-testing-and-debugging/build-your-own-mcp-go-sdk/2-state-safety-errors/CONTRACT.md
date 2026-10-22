# Log Triage contract

**Related**:

> [README](README.md): This practice: tasks, prompts, checks
> [log-formats](../docs/log-formats.md): How every log line is read; part of this contract
> [hostile-inputs](hostile-inputs.md): Inputs that must be refused, to try in the Inspector

What your server publishes after this practice. Every name, type, bound, default, annotation, output field and text below is exact: the checks in `checks/` compare them. Tool descriptions are yours to write; only their length is checked.

## Links
- [MCP specification: Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)
- [MCP specification: Resources](https://modelcontextprotocol.io/specification/2026-07-28/server/resources)

**Internal**:

- `docs/log-formats.md`
- `2-state-safety-errors/hostile-inputs.md`
- `2-state-safety-errors/checks/`

## 1. Server
| Item | Value |
|---|---|
| Name | `log-triage` |
| Version | `0.2.0` |
| Python | package `logtriage`, console script `log-triage`, Python 3.13, `fastmcp==4.0.3` |
| Go | module `github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server`, command `cmd/log-triage`, Go 1.26, `github.com/modelcontextprotocol/go-sdk v1.7.0` |
| Transport | stdio |
| `instructions` | the text in section 7 |

### Command line
| Flag | Meaning | Default |
|---|---|---|
| `--root DIR` | The log root. Every `path` argument is resolved under it. Required | none |
| `--reports DIR` | Where `save_finding` writes. Created if missing | `reports` in the working directory |
| `--read-only` | Do not register `save_finding` | off |
| `--scan-ttl SECONDS` | How long a `scan_id` works, 1 or more | `900` |

A missing or unreadable `--root` makes the server exit with code 2 and print one line on stderr: `log root "<DIR>" is not a readable folder`, with `<DIR>` as given on the command line. A `--scan-ttl` below 1 also exits with code 2; that message is yours.

Nothing the server prints goes to stdout except the protocol.

## 2. Reading logs
`docs/log-formats.md` is part of this contract. Format detection, the nine formats, untimed lines, times and message groups are specified there.

### Paths
- A `path` argument is a string relative to the log root, or an absolute path inside it. `.` or an empty string means the root.
- Output paths are always relative to the root, `/`-separated, with no leading `./`. The root itself is `.`.
- A folder means every regular file below it, recursively, in byte order of the relative path. Files and folders whose names start with `.` are skipped.
- Every `path` argument and every evidence `file` is screened where the path is built. It is refused with E1 (section 5) when it holds a NUL byte, is absolute and outside the root, has a `..` that leaves the root, or passes through a symlink: the file itself or any folder on the way.
- "On the way" means below the log root. The root itself is resolved once at startup and may sit under symlinks, as `/tmp` and `/var` do on macOS; a server that refuses those refuses everything.
- A path that passes screening and doesn't exist is refused with E2.
- A symlinked entry met while walking a folder, file or folder, is never followed. A scan skips it with reason `symlink`; `list_logs` leaves it out.

### Files
- A name ending in `.gz` is gzip; it is decompressed as a stream. Everything else is read as it is.
- Content is decoded as UTF-8 with invalid bytes replaced. `\r` before `\n` is dropped. Line numbers are 1-based in the decoded content.
- A line longer than 65,536 characters is cut there.
- A file larger than 64 MiB decompressed, which means more than 67,108,864 bytes, is skipped with reason `too large`. A plain file is judged by its length as `stat` reports it, so a sparse file counts in full; a `.gz` file is judged while it is read.
- A `.gz` file that fails to decode is skipped with reason `unreadable`.
- A file of format `unknown` is skipped with reason `unknown format`.
- A scan checks in this order: a symlinked entry first, then detection (a `.gz` file that fails to decode within the lines detection reads is `unreadable`), then `unreadable` or `too large` met while reading. A skipped file adds nothing to any count, group, match or `lines_scanned`, even when it was skipped halfway through.
- `list_logs` has no `skipped`. It lists a file of format `unknown`, lists a `.gz` file that fails to decode within the lines detection reads with format `unknown`, and doesn't apply the size cap. A `.gz` file that fails only later may list with its real format.
- `skipped` entries are `{"path": str, "reason": str}`, reasons `unknown format`, `symlink`, `too large` and `unreadable`, at most 20 listed, sorted by path.
- Every length and cut in this contract (1,000, 200, 500, 65,536 and the parameter limits) counts Unicode code points; in Go, runes of the decoded content.

### Times
- Output: `YYYY-MM-DDTHH:MM:SSZ`, UTC, fractions cut.
- Input `since` and `until`: `YYYY-MM-DD` (midnight UTC), or `YYYY-MM-DDTHH:MM` with optional `:SS`, an optional fraction after the seconds, and an optional `Z` or `±HH:MM`. No zone means UTC.
- A `since` or `until` that doesn't follow this grammar, or names a date or time that doesn't exist, is refused with E3. When both are bad, `since` is reported.
- `until` must be later than `since` when both are given; an equal or earlier `until` is refused with E4.
- Every time is cut to whole seconds when it is read: line times, run headers, `since` and `until`. Windows, `first_seen`, `last_seen` and the group order compare the cut values.
- A window is `since <= time < until`. Either end may be omitted. With neither end given, every line is in the window, lines of files that have no time included. With either end given, a line without a time is outside it.
- Timestamps are trusted as written. No file is skipped because of the date in its name.

## 3. Tools
### Output rules
- Every tool publishes an `outputSchema` and returns `structuredContent` that validates against it, plus one text block holding the same object as JSON. The text is compared parsed, not as bytes, so key order is free. A successful result has `isError` absent or `false`.
- Parameter types are exactly as the tables give them. A parameter written "or null" publishes a type list with `null` (`["null", "string"]`) or an `anyOf` with a `null` branch, and its `default: null` may be present or absent. Any other parameter publishes its plain type and nothing else. In Go, a pointer or a slice where a table says a plain type publishes a nullable type and fails the checks; go-sdk fills schema defaults itself, so `limit`, `offset`, `before` and `after` need no pointer.
- Parameters with a default are not in `required`.
- Lists in results are `[]` when empty, never `null`. An output schema may still type a list or an object as nullable; the value is what is checked.
- A paged or capped list comes with `total` (all that exist), `shown` (in this answer) and `note`.
  - A tool with `offset`: `note` is `""` when no later page exists (`offset + shown >= total`, which includes an `offset` past the end with `shown` 0). Otherwise it is `Showing <shown> of <total> <things>. Pass offset=<next> for more.`, with `<next>` = `offset + shown` and `<things>` = `files`, `matches` or `lines`.
  - `summarize_logs`: `note` is `""` when every group is shown. Otherwise it is `Showing <shown> of <total> groups. Raise limit (up to 100) or narrow the path or the window.`
  - `group_lines` has its own addition for large groups (see there).
- Line text in any output is cut to 500 characters. No output field holds a log line except `example.text` in `summarize_logs` and `text` in `search_logs`, `group_lines` and `read_lines`.

### Titles and annotations
| Tool | `title` | `readOnlyHint` | `destructiveHint` | `idempotentHint` | `openWorldHint` |
|---|---|---|---|---|---|
| `list_logs` | List log files | true | | | false |
| `summarize_logs` | Summarize logs | true | | | false |
| `search_logs` | Search logs | true | | | false |
| `group_lines` | Show a group's lines | true | | | false |
| `read_lines` | Read lines around a line | true | | | false |
| `save_finding` | Save a finding | false | false | false | false |

- The title may sit on the tool, in its annotations, or both; wherever it appears, it is the table's text.
- Read-only tools set `readOnlyHint` and `openWorldHint` and leave `destructiveHint` out, because the specification ignores it when `readOnlyHint` is true.
- An absent `readOnlyHint` or `idempotentHint` means `false`, and the checks treat the two alike. go-sdk v1.7.0 always writes both, so a Go server shows `idempotentHint: false` on its read-only tools.
- `destructiveHint` and `openWorldHint` default to `true`, so every `false` in the table is written out.
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
Counts and message groups for a file or folder in a window. The tool that answers "what went wrong here" without reading every line. Every call is a new scan with its own `scan_id` (section 6).

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
| `scan_id` | string | `s_` and 16 lowercase hex characters, random |
| `expires_at` | string | The call time plus `--scan-ttl`, in the output format |
| `path`, `since`, `until`, `min_level` | | The request, normalized; times in the output format, `null` when not given |
| `files_scanned` | integer | Files read, skipped ones excluded |
| `skipped` | list | See section 2 |
| `lines_scanned` | integer | Every line read, skipped files excluded |
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

The pattern is searched anywhere in the raw line, not only in the message. A pattern that doesn't compile is refused with E5. A line matches when its time is in the window, its level is at or above `min_level`, and the pattern is found. Output: `path`, `pattern`, `matches` (list of `file`, `line`, `time` or null, `level`, `text`), `total` (every match), `shown`, `note`. Order: file path, then line.

### group_lines
The lines of one group from a scan, without scanning again.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `scan_id` | string | pattern `^s_[0-9a-f]{16}$` | required |
| `group_id` | string | pattern `^[0-9a-f]{12}$` | required |
| `limit` | integer | 1 to 200 | 50 |
| `offset` | integer | 0 or more | 0 |

Output: `scan_id`, `group_id`, `pattern`, `lines` (list of `file`, `line`, `time` or null, `text`), `total`, `shown`, `note`. Lines come in scan order: file path, then line. `time` is the line's time as a scan reads it (section 2, Times), and `text` is the raw line.

- A scan keeps every group at or above its `min_level` (the groups `total_groups` counts), not only the `limit` it showed. Any other `group_id` is refused with E7. An unknown or expired `scan_id` is refused with E6.
- A scan keeps the positions of at most 10,000 lines per group, the first ones in scan order. The lines are read back from the files when `group_lines` asks for them.
- `total` is the group's full count. For a group with more than 10,000 lines, "a later page exists" is decided against the 10,000 kept lines, while `<total>` in the paging sentence stays the full count; the note then ends with `Only the first 10000 lines are kept.`, after a space when the paging sentence is there and on its own when it isn't. So `offset` 10000 gives `shown` 0 and the note `Only the first 10000 lines are kept.`

### read_lines
Context around one line of one file.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `path` | string | max 500 chars | required |
| `line` | integer | 1 or more | required |
| `before` | integer | 0 to 50 | 5 |
| `after` | integer | 0 to 200 | 20 |

Output: `file` (the normalized path), `start`, `end`, `total_lines`, `lines` (list of `line`, `text`). The window runs from `line - before` to `line + after`, clipped to the file. Any file under the root can be read, format `unknown` included. A folder is refused with E10, a `line` past the last line with E8. What happens on a file over the size cap is not specified, as long as the server keeps running; a `.gz` file that isn't gzip is an unexpected error (section 5).

### save_finding
Writes one finding as a markdown file. The only write.

| Parameter | Type | Constraints | Default |
|---|---|---|---|
| `title` | string | 3 to 120 chars | required |
| `severity` | enum `low`, `medium`, `high`, `critical` | | required |
| `summary` | string | 1 to 4,000 chars | required |
| `evidence` | list of `file` (string, max 500), `line` (integer, 1 or more), `note` (string, 1 to 300 chars); all three required in every item, and no other key | 1 to 20 items | required |

An evidence item is a closed object: its schema has `additionalProperties: false`, and an item with any key other than `file`, `line` and `note` is refused before your code runs (section 5), with nothing written.

The file name is the slug of the title plus `.md`. The slug, in this order:

1. Lowercase the ASCII letters.
2. Every run of characters other than `a` to `z` and `0` to `9` becomes one `-`.
3. Trim `-` at both ends.
4. Cut to 60 characters.
5. Trim a trailing `-` again.
6. An empty slug becomes `finding`.

Checks behind the schema, in this order, with nothing written when one fails:

1. A file with that name already exists under `--reports`: E9.
2. Each evidence item, in the given order: its `file` is screened like a `path` (E1, E2), must be a file (E10), and its `line` must exist in that file (E8).

The file is written under `--reports` and nowhere else. Its content:

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

With `--read-only`, `save_finding` is not registered, and every other tool and every resource stays.

## 4. Resources
| URI | Kind | MIME type | Content |
|---|---|---|---|
| `triage://formats` | resource | `application/json` | `{"formats": [{"name", "levels", "timestamp", "example"}]}`, the nine formats in the order of `docs/log-formats.md` |
| `triage://reports` | resource | `application/json` | `{"count": n, "reports": [{"name", "title", "severity", "created"}]}` sorted by `name` |
| `triage://reports/{name}` | template | `text/markdown` | The saved file, exactly |
| `triage://scans/{scan_id}` | template | `application/json` | The `summarize_logs` result of that scan |

### triage://formats
- `name` is the format's name.
- `levels` lists the normalized levels the format's rules can give, from `critical` down to `debug`, with `unknown` last on every format, because a line without the format's shape is `unknown`. So `syslog` is `["unknown"]` and `nginx-access` is `["error", "warning", "info", "unknown"]`.
- `example` is the first line of the format's sample block in `docs/log-formats.md` that matches the format's sniff pattern. For `redis` that is the second sample line, because the first has no redis shape.
- `timestamp` is exactly:

| Format | `timestamp` |
|---|---|
| `app` | `yyyy-mm-ddTHH:MM:SS.fffZ` |
| `nginx-access` | `dd/Mon/yyyy:HH:MM:SS ±zzzz` |
| `nginx-error` | `yyyy/mm/dd HH:MM:SS` |
| `php-fpm` | `dd-Mon-yyyy HH:MM:SS` |
| `postgres` | `yyyy-mm-dd HH:MM:SS.fff UTC` |
| `redis` | `dd Mon yyyy HH:MM:SS.fff` |
| `syslog` | `yyyy-mm-ddTHH:MM:SS.fff±hh:mm` |
| `pytest` | `run header` |
| `gotest` | `run header` |

### triage://reports
- The list holds regular `*.md` files directly under `--reports` whose name before `.md` contains no dot; dot files, embedded-dot names and symlinks are left out.
- `name` is the file name without `.md`. `title`, `severity` and `created` come from the file's `# `, `- Severity: ` and `- Saved: ` lines. A `.md` file without those lines is left out.
- No reports, or no `--reports` folder yet, gives `{"count": 0, "reports": []}`.
- `triage://reports/{name}` reads only a name the list would show. Any other name (`..`, `a.b`, a left-out file) is not found.

### Not found
An unknown report, and an unknown or expired scan, answer the protocol's resource-not-found error: JSON-RPC code `-32602`. The error carries the URI in its message or in `data.uri`, and nothing else from the server. A not-found error your server raises itself uses the message `Resource not found: <uri>` and `data` `{"uri": "<uri>"}`. A URI that matches no resource and no template is answered by the SDK in the same kind.

### Freshness
Every `resources/list`, `resources/templates/list` and `resources/read` result carries `ttlMs: 30000` and `cacheScope: "private"`.

## 5. Errors
### Refused before your code runs
A value out of bounds, a value outside an enum, a string that doesn't match its pattern, an unknown argument, an unknown key in an evidence item, a missing required argument and an unknown tool are refused by the SDK. The checks accept either kind of refusal: a JSON-RPC error, or a result with `isError: true` and no `structuredContent`. They never check the wording.

### Refused by your tool
A result with `isError: true`, no `structuredContent`, and exactly this text, placeholders filled:

| ID | Text | Where |
|---|---|---|
| E1 | `path "<path>" is outside the log root. Pass a path relative to the log root; list_logs with path "." shows what is there.` | any `path` or evidence `file` |
| E2 | `path "<path>" does not exist under the log root. Call list_logs to see what is there.` | any `path` or evidence `file` |
| E3 | `<field> must be a date like 2026-01-31 or a time like 2026-01-31T14:00:00Z.` (`<field>` is `since` or `until`) | `summarize_logs`, `search_logs` |
| E4 | `until must be later than since.` | `summarize_logs`, `search_logs` |
| E5 | `<field> is not a valid regular expression. Escape special characters such as ( [ . * with a backslash, or search for a plain word.` (`<field>` is `pattern`) | `search_logs` |
| E6 | `scan "<scan_id>" is unknown or has expired. Call summarize_logs again for a new scan_id.` | `group_lines` |
| E7 | `group "<group_id>" is not in scan "<scan_id>". Use a group_id from that scan's groups.` | `group_lines` |
| E8 | `line <line> is past the end of "<file>", which has <total> lines.` | `read_lines`, evidence `line` |
| E9 | `a finding named "<name>" already exists. Choose a different title.` | `save_finding` |
| E10 | `"<path>" is a folder. This needs one file; call list_logs to pick one.` | `read_lines`, evidence `file` |

- E1 and E2 quote `<path>` as the caller passed it. E8 and E10 quote the normalized relative path, `.` for the root, so an empty `path` on `read_lines` answers `"." is a folder. ...`.
- A quoted value is its raw characters between plain `"` quotes, with no escaping beyond what JSON does on the wire. The checks compare the decoded text.
- The scan cap is not an error: the 21st scan drops the oldest without a word, and the dropped id then answers E6.
- No error text shows the log root's absolute path, unless the caller passed it.

### Unexpected errors
Any other exception is masked: the caller sees a generic tool error with no exception message, no absolute path and no stack trace. The detail may go to stderr, never to the caller. `read_lines` on a `.gz` file that isn't gzip is such an unexpected error: it is masked, and it comes before E8.

## 6. State
- A `scan_id` is random and says nothing about other ids.
- It works until `expires_at` and is unknown after that (E6).
- It belongs to the caller that made it. Over stdio every call comes from one caller, `local`. An id that belongs to another caller answers E6, like an unknown one.
- Each caller holds at most 20 scans; the 21st drops the oldest.
- The store is safe under concurrent calls.
- A scan keeps the normalized request, the file list, the window, the groups at or above `min_level` with the positions of their lines (at most 10,000 per group), and the summary it returned. Nothing else is cached.

## 7. Instructions
The practice 1 text, a space, and the scan sentence, under 800 characters in all:

> Log Triage reads log files under one folder. Start with list_logs to see what is there. Use summarize_logs on a file or folder, with since and until when you know the time, to get counts and message groups instead of raw lines. Use search_logs to find the lines that match a pattern. Record a conclusion with save_finding and cite a file and line for every piece of evidence. All times are UTC. summarize_logs returns a scan_id that works for 15 minutes; pass it to group_lines to read one group's lines without scanning again. Use read_lines for the lines around one line. Saved findings are listed at triage://reports.

`15 minutes` is filled from `--scan-ttl` with the exact configured duration. Exact whole-minute values use minutes, with `1 minute` when the value is 60 seconds. Other values use seconds, with `1 second` when the value is 1. `--scan-ttl 150` gives `150 seconds`; `--scan-ttl 5` gives `5 seconds`.
