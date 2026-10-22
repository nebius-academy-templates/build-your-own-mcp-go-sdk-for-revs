# Log formats

**Related**:

> [s3-logs](s3-logs.md): What the log bundle in `s3_logs/` holds

How Log Triage turns a file into lines, each with a time, a level and a message, and how it turns messages into groups. Every rule here is exact: the check suites compare your server's answers with values computed from these rules. The sample lines are made up (host `myhost`, January 2026, address `192.0.2.10`); the real files in `s3_logs/` follow the same shapes.

## Links
- [Python `re`](https://docs.python.org/3/library/re.html)
- [Go `regexp` syntax](https://pkg.go.dev/regexp/syntax)
- [RFC 3339, date and time on the internet](https://www.rfc-editor.org/rfc/rfc3339)

**Internal**:

- `docs/s3-logs.md`

## Reading a file
- A name ending in `.gz` is gzip: decompress it as a stream. Read every other file as it is.
- Decode the content as UTF-8, replacing invalid bytes. Drop a `\r` that comes right before `\n`.
- Lines are numbered from 1 in the decoded content. A final `\n` doesn't start another line.
- Every length and cut in these rules counts Unicode code points. In Go, count runes of the decoded content, not bytes.

## Regular expressions
The patterns below work the same in Python `re` and Go `regexp` as long as Python compiles them with `re.ASCII`. Without that flag, Python's `\d`, `\w` and `\b` also match non-ASCII digits and letters, and your patterns and `group_id` values stop matching the ones the checks expect.

A pattern is searched in the line (Python `re.search`, Go `MatchString`), so one without `^` can match anywhere.

## Levels
Every line gets one of six levels. From low to high:

| Level | Meaning |
|---|---|
| `unknown` | the format says nothing about severity for this line |
| `debug` | tracing detail |
| `info` | normal operation |
| `warning` | something unusual that didn't fail |
| `error` | a failure |
| `critical` | a failure that stops a service or needs a human now |

## Detecting the format
1. Take up to the first 50 non-empty lines of the file. A line is empty when it has zero length after the `\r` drop; a line of spaces is not empty.
2. For each format, in the order of the table below, count how many of those lines its sniff pattern matches.
3. The highest count wins. A tie goes to the format that comes first in the table.
4. If no pattern matches any line, the format is `unknown`. A file of format `unknown` is still listed, but every scan skips it with reason `unknown format`. The `host.json` files in `s3_logs/` are `unknown`.

| Order | Format | Typical file names in `s3_logs/` |
|---|---|---|
| 1 | `app` | `app.log` |
| 2 | `nginx-access` | `nginx-access.log` |
| 3 | `nginx-error` | `nginx-error.log` |
| 4 | `php-fpm` | `php-fpm.log` |
| 5 | `postgres` | `postgres.log` |
| 6 | `redis` | `redis.log` |
| 7 | `syslog` | `syslog.log`, `auth.log` |
| 8 | `pytest` | `pytest.log` |
| 9 | `gotest` | `gotest.log` |

The file name never decides the format; the content does. `pytest` and `gotest` files both start with the same run header, and the lines after it decide, which is why detection looks at 50 lines.

## The nine formats
For each format: the sniff pattern, a sample line, and how to read the time, the level and the message. For the first seven formats a line **has the format's shape** when the sniff pattern matches it (`nginx-access` also needs its status, see below). A line without its format's shape follows the rule under "Lines without their format's shape" further down.

### app
```text
^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z (DEBUG|INFO|WARN|ERROR|FATAL) \S+ 
```

The pattern ends with a space.

```text
2026-01-15T10:04:12.345Z ERROR billing charge failed order=A-1042 amount=19.99 client=192.0.2.10 trace=3f2a9c1e7b6d4a58
```

| Part | Rule | Sample |
|---|---|---|
| Time | the first field, already UTC | `2026-01-15T10:04:12Z` |
| Level | `FATAL` critical, `ERROR` error, `WARN` warning, `INFO` info, `DEBUG` debug | `error` |
| Message | the text after the third field (the service name) and its space | `charge failed order=A-1042 amount=19.99 client=192.0.2.10 trace=3f2a9c1e7b6d4a58` |

### nginx-access
```text
^\S+ \S+ \S+ \[\d{2}/[A-Z][a-z]{2}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}\] "
```

```text
192.0.2.10 - - [15/Jan/2026:10:04:12 +0100] "GET /catalog/items?page=2 HTTP/1.1" 502 157 "-" "curl/8.5.0"
192.0.2.10 - - [15/Jan/2026:10:04:13 +0100] "\x16\x03" 400 150 "-" "-"
```

| Part | Rule | Sample, line 1 | Sample, line 2 |
|---|---|---|---|
| Time | `[dd/Mon/yyyy:HH:MM:SS ±zzzz]`; apply the offset to get UTC | `2026-01-15T09:04:12Z` | `2026-01-15T09:04:13Z` |
| Level | status 500 and up error, 400 to 499 warning, anything else info | `error` | `warning` |
| Message | `METHOD PATH STATUS` | `GET /catalog/items 502` | `- - 400` |

- The request is the text between the first `"` and the next `"`.
- The status is the three-digit field right after the request's closing quote and a space. A line without it doesn't have the format's shape.
- Split the request on single spaces. When that gives exactly three parts, `METHOD` is the first, `PATH` is the second cut at the first `?`, and the third (the protocol) is dropped. Otherwise the message is `- - STATUS`.

### nginx-error
```text
^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} \[[a-z]+\] \d+#\d+: 
```

The pattern ends with a space.

```text
2026/01/15 10:04:12 [crit] 41#41: *7 open() "/var/www/myhost/cache/a1.tmp" failed (13: Permission denied), client: 192.0.2.10, server: myhost
```

| Part | Rule | Sample |
|---|---|---|
| Time | `yyyy/mm/dd HH:MM:SS`, UTC | `2026-01-15T10:04:12Z` |
| Level | `emerg`, `alert`, `crit` critical; `error` error; `warn` warning; `notice`, `info` info; `debug` debug | `critical` |
| Message | the text after `pid#tid: `, without a leading `*N ` (the connection number) when there is one | `open() "/var/www/myhost/cache/a1.tmp" failed (13: Permission denied), client: 192.0.2.10, server: myhost` |

### php-fpm
```text
^\[\d{2}-[A-Z][a-z]{2}-\d{4} \d{2}:\d{2}:\d{2}\] [A-Z]+: 
```

The pattern ends with a space.

```text
[15-Jan-2026 10:04:12] WARNING: [pool www] child 38 exited on signal 9 (SIGKILL) after 12.5 seconds from start
```

| Part | Rule | Sample |
|---|---|---|
| Time | `[dd-Mon-yyyy HH:MM:SS]`, UTC | `2026-01-15T10:04:12Z` |
| Level | `ALERT` critical, `ERROR` error, `WARNING` warning, `NOTICE` info, `DEBUG` debug | `warning` |
| Message | the text after `LEVEL: ` | `[pool www] child 38 exited on signal 9 (SIGKILL) after 12.5 seconds from start` |

### postgres
```text
^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+ UTC \[\d+\] [A-Z0-9]+: 
```

The pattern ends with a space.

```text
2026-01-15 10:04:12.345 UTC [2231] ERROR:  duplicate key value violates unique constraint "users_email_key"
2026-01-15 10:04:12.345 UTC [2231] DETAIL:  Key (email)=(ann@example.com) already exists.
```

| Part | Rule | Sample, line 1 | Sample, line 2 |
|---|---|---|---|
| Time | `yyyy-mm-dd HH:MM:SS.fff UTC` | `2026-01-15T10:04:12Z` | `2026-01-15T10:04:12Z` |
| Level | see below | `error` | `error` |
| Message | the text after `LEVEL:` and all the spaces that follow it | `duplicate key value violates unique constraint "users_email_key"` | `Key (email)=(ann@example.com) already exists.` |

Levels: `PANIC`, `FATAL` critical; `ERROR` error; `WARNING` warning; `LOG`, `INFO`, `NOTICE` info; `DEBUG1` to `DEBUG5` debug. `DETAIL`, `HINT`, `STATEMENT`, `CONTEXT`, `QUERY` and `LOCATION` lines explain the line before them, so they take the level of the previous line in the file, whatever that line's level turned out to be. On the first line of a file they get `unknown`.

### redis
```text
^\d+:[XCSM] \d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2}\.\d+ [.\-*#] 
```

The pattern ends with a space.

```text
1:signal-handler (1768471452) Received SIGINT scheduling shutdown...
1:M 15 Jan 2026 10:04:12.345 # WARNING overcommit_memory is set to 0! Background save may fail under low memory condition.
1:M 15 Jan 2026 10:04:12.346 * DB saved on disk
```

| Part | Rule | Line 1 | Line 2 | Line 3 |
|---|---|---|---|---|
| Time | `dd Mon yyyy HH:MM:SS.fff`, UTC | `2026-01-15T10:04:12Z`, borrowed | `2026-01-15T10:04:12Z` | `2026-01-15T10:04:12Z` |
| Level | the mark: `#` warning, `*` info, `-` and `.` debug | `unknown` | `warning` | `info` |
| Message | the text after the mark and its space | the whole line, trimmed | `WARNING overcommit_memory is set to 0! Background save may fail under low memory condition.` | `DB saved on disk` |

Line 1 has no redis shape. Its number in brackets is a Unix time, not a timestamp this format reads, so the line has no time of its own and borrows one (see "Untimed lines" further down).

### syslog
```text
^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2} \S+ [^\s:]+: 
```

The pattern ends with a space. `syslog.log` and `auth.log` are both this format.

```text
2026-01-15T10:04:12.345678+02:00 myhost sshd[812]: Accepted publickey for deploy from 192.0.2.10 port 50122 ssh2
```

| Part | Rule | Sample |
|---|---|---|
| Time | RFC 3339 with an offset; apply the offset to get UTC | `2026-01-15T08:04:12Z` |
| Level | always `unknown` | `unknown` |
| Message | `PROGRAM: TEXT`: the program token (the third field) with a trailing `[digits]` removed, then `: `, then the text after the pattern | `sshd: Accepted publickey for deploy from 192.0.2.10 port 50122 ssh2` |

### pytest
```text
^=== run \S+ ===$|::\S* (PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)
```

```text
=== run 2026-01-15T10:00:00Z ===
============================= test session starts ==============================
tests/test_cart.py::test_add_item PASSED                                  [ 50%]
tests/test_cart.py::test_remove_item FAILED                               [100%]
E       assert 404 == 200
FAILED tests/test_cart.py::test_remove_item - assert 404 == 200
```

- **Time.** Only the run header (`=== run <time> ===`) has a time (see "Run headers" further down). Every other line borrows one (see "Untimed lines"), so all six sample lines are at `2026-01-15T10:00:00Z`.
- **Level**, first rule that applies:
  1. The run header: `info`.
  2. The result after `::<test id> ` (the second alternative of the sniff pattern) is `FAILED` or `ERROR`, or the line starts with `FAILED ` or `ERROR `: `error`.
  3. The result is `PASSED`, `SKIPPED`, `XFAIL` or `XPASS`: `info`.
  4. Anything else: `unknown`.
- **Message.** The whole line, trimmed of `[ \t\n\r\f\v]` at both ends.
- "Starts with" is tested on the line as it is, so an indented `FAILED ` doesn't count.

Sample levels, in order: `info`, `unknown`, `info`, `error`, `unknown`, `error`.

### gotest
```text
^=== run \S+ ===$|^\s*(--- (PASS|FAIL|SKIP):|=== (RUN|PAUSE|CONT) )
```

```text
=== run 2026-01-15T10:00:00Z ===
=== RUN   TestCart
=== RUN   TestCart/add_item
    cart_test.go:42: expected 200, got 404
    --- FAIL: TestCart/add_item (0.01s)
--- FAIL: TestCart (0.01s)
FAIL
FAIL	example.com/shop	0.015s
ok  	example.com/shop/api	0.004s
```

The last two lines have tabs between their fields.

- **Time.** As in `pytest`: the run header has one, every other line borrows it.
- **Level**, first rule that applies:
  1. The run header: `info`.
  2. `--- FAIL:` after optional leading whitespace, or a line that starts with `FAIL`: `error`.
  3. `--- PASS:` or `--- SKIP:` after optional leading whitespace, or a line that starts with `ok `: `info`.
  4. `=== RUN `, `=== PAUSE ` or `=== CONT ` after optional leading whitespace: `debug`.
  5. Anything else: `unknown`.
- **Message.** The whole line, trimmed of `[ \t\n\r\f\v]` at both ends.
- "Starts with" is tested on the line as it is, with no leading whitespace allowed. The `---` and `===` markers allow leading whitespace, as the sniff pattern does.

Sample levels, in order: `info`, `debug`, `debug`, `unknown`, `error`, `error`, `error`, `error`, `info`.

## Rules for every format
### Lines without their format's shape
A line that doesn't have its format's timestamp and level layout gets:

- level `unknown`,
- no time of its own,
- the whole line, trimmed of `[ \t\n\r\f\v]` at both ends, as its message.

The redis `signal-handler` line above is one. For `pytest` and `gotest` this is simply their "anything else" rule.

### Unlisted level words and impossible times
- A level word that isn't in a format's list (for example `FOO:` in a `postgres` line) gives `unknown`.
- A timestamp with the right shape that isn't a real date or time makes the line untimed; its level is still read. Real means: month `01` to `12` (or `Jan` to `Dec`, exactly so capitalized), a day that exists in that month and year, hour `00` to `23`, minute and second `00` to `59`, and an offset of at most `23:59`.

### Run headers
A `pytest` or `gotest` run header is a whole line `=== run <time> ===`. The time is the text between `=== run ` and ` ===`, read with the grammar the tools accept for `since` and `until`:

- `YYYY-MM-DD`, meaning midnight UTC, or
- `YYYY-MM-DDTHH:MM`, then optionally `:SS`, then optionally a fraction after the seconds (`.` and digits), then optionally `Z` or `±HH:MM`. No zone means UTC.

A header whose time doesn't read that way is an untimed line with level `info`.

### Untimed lines
A line without its own time takes the time of the nearest earlier line in the same file that has one. Lines before the file's first timed line take that first timed line's time. A file with no timed line at all has no time. Its lines are counted as scanned. They fall outside any window that has an end, and count as in the window only when neither `since` nor `until` is given. Their groups have no `first_seen` or `last_seen`.

So every line of a test run carries its run header's time, and a line at the top of a file carries the time of the first dated line after it.

## Times
- Cut every time to whole seconds when you read it: line times, run headers, `since` and `until`. Compare only the cut values.
- Convert to UTC with the line's offset when it has one (`nginx-access`, `syslog`, run headers). The other formats are UTC as written.
- Write times as `YYYY-MM-DDTHH:MM:SSZ`.
- A window is `since <= time < until`; either end may be missing.
- Trust each line's own time. A file's name never moves a line in or out of a window.

## Message groups
A group is every line in the window with the same format, level and pattern. The pattern is the message after these seven steps, in this order:

1. Cut to 1,000 characters.
2. Replace every match of `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` with `<uuid>`.
3. Replace every match of `\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b` with `<ip>`.
4. Replace every match of `\b[0-9a-fA-F]{8,}\b` with `<hex>`.
5. Replace every match of `\d+(\.\d+)?` with `<n>`.
6. Replace every run of `[ \t\n\r\f\v]` with one space, then trim spaces at both ends. The class is spelled out because Python's `\s` includes `\v` and Go's doesn't.
7. Cut to 200 characters.

`group_id` is the first 12 hex characters of the SHA-256 of `format + "\n" + level + "\n" + pattern`, encoded as UTF-8. The same lines give the same id in every correct server.

Groups are ordered by `count`, highest first, then by `first_seen`, earliest first, with groups that have no `first_seen` last, then by `group_id`.

### Worked examples
| Format | Level | Message | Pattern | `group_id` |
|---|---|---|---|---|
| `app` | `error` | `charge failed order=A-1042 amount=19.99 client=192.0.2.10 trace=3f2a9c1e7b6d4a58` | `charge failed order=A-<n> amount=<n> client=<ip> trace=<hex>` | `6ca6f297d79f` |
| `app` | `info` | `session 0b7c4f5e-2d1a-4c3b-9e8f-7a6b5c4d3e2f expired after 1800s` | `session <uuid> expired after <n>s` | `c135c047cc96` |
| `nginx-error` | `error` | `upstream 192.0.2.10:8080 timed out (110: Connection timed out)` | `upstream <ip> timed out (<n>: Connection timed out)` | `e0621a880600` |
| `app` | `warning` | `cache key a1b2c3d4 missed, key a1b2c3 kept` | `cache key <hex> missed, key a<n>b<n>c<n> kept` | `0878c8c9a550` |
| `app` | `info` | `worker v2.10.3 started in 0.25s` | `worker v<n>.<n> started in <n>s` | `b44973b52fca` |
| `nginx-access` | `error` | `GET /catalog/items 502` | `GET /catalog/items <n>` | `05b889816843` |
| `syslog` | `unknown` | `sshd: Accepted publickey for deploy from 192.0.2.10 port 50122 ssh2` | `sshd: Accepted publickey for deploy from <ip> port <n> ssh<n>` | `a70ece346610` |
| `redis` | `unknown` | `1:signal-handler (1768471452) Received SIGINT scheduling shutdown...` | `<n>:signal-handler (<hex>) Received SIGINT scheduling shutdown...` | `630d35dd7e9d` |
| `gotest` | `unknown` | `cart_test.go:42: expected 200, got 404` | `cart_test.go:<n>: expected <n>, got <n>` | `3a2fb9037cde` |

What the examples show:

- The order of the steps matters. `a1b2c3d4` is eight hex characters between word boundaries, so step 4 takes it whole. `a1b2c3` is too short for step 4, so step 5 replaces its digits one by one.
- Step 3 takes the port with the address. Step 5 then takes the `110`.
- `2.10.3` becomes `<n>.<n>`: step 5 matches `2.10` first, then `3`.
- The epoch in the redis line has ten digits, so step 4 turns it into `<hex>` before step 5 sees it.
- The same pattern at a different level, or in a different format, is a different group with a different `group_id`.

To check your own code, feed it the sample lines on this page and compare with the tables.
