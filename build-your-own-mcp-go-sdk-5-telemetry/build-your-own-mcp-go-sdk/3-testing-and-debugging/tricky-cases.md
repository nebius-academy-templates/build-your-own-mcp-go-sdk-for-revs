# Tricky cases

**Related**:

> [log-formats](../docs/log-formats.md): The rules these cases exercise
> [README](README.md): Project guide and the unit-test layer

Made-up log lines that sit on the edges of the rules in `docs/log-formats.md`, each with what a correct reader makes of it. Every expected value below was computed from those rules, not from a server. Each block is one small file: the lines are read in order, so an untimed line takes a neighbour's time and a postgres follow-up line takes the level of the line before it.

## Links
**Internal**:

- `docs/log-formats.md`
- `3-testing-and-debugging/README.md`

## How to read the tables
- **Time** is the line's time after untimed lines are filled in, in the output format; `null` means the file has no time at all.
- **Pattern** is the message after the seven grouping steps; **group_id** is the first 12 hex characters of the SHA-256 of `format`, a newline, `level`, a newline and the pattern.
- In the line texts, `\t` stands for a tab character, and `\|` for a `|` inside a table.

## app: `billing.log`
```text
2026-01-15T10:04:12.345Z WARN billing retry 3 of 5 for order 7f3a9c2e-1b4d-4e8a-9c3f-2d1e6b7a8c90 from 192.0.2.10:44321
2026-01-15T10:04:13.999Z ERROR billing   charge   failed\tfor  client 0xDEADBEEF hash deadbeefcafe
2026-01-15T10:04:14.500Z TRACE billing an unlisted level word
2026-01-15T10:04:15.000Z INFO greeter привет 42 мир
2026-01-15T10:04:16.000Z INFO db slow query <1,100 x characters>
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:04:12Z | `warning` | `retry 3 of 5 for order 7f3a9c2e-1b4d-4e8a-9c3f-2d1e6b7a8c90 from 192.0.2.10:44321` | `retry <n> of <n> for order <uuid> from <ip>` | `33ddc1fe54a2` |
| 2 | 2026-01-15T10:04:13Z | `error` | `  charge   failed\tfor  client 0xDEADBEEF hash deadbeefcafe` | `charge failed for client <n>xDEADBEEF hash <hex>` | `d288f6427fda` |
| 3 | 2026-01-15T10:04:13Z | `unknown` | `2026-01-15T10:04:14.500Z TRACE billing an unlisted level word` | `<n>-<n>-<n>T<n>:<n>:<n>Z TRACE billing an unlisted level word` | `d0dead41d05f` |
| 4 | 2026-01-15T10:04:15Z | `info` | `привет 42 мир` | `привет <n> мир` | `01529d0da3e4` |
| 5 | 2026-01-15T10:04:16Z | `info` | `slow query ` and 1,100 `x` | `slow query ` and 189 `x` | `d4c2c0861519` |

- Line 1: a UUID, an address with a port, and numbers.
- Line 2: whitespace runs and a tab; `0xDEADBEEF` has no word boundary before `D`, so only its `0` is a number, while `deadbeefcafe` is hex.
- Line 3: `TRACE` isn't in the level list, so the line has no app shape: `unknown`, the whole line as the message, and the time of the line before.
- Line 4: non-ASCII letters stay; the cuts count code points.
- Line 5: a 1,111-character message: cut to 1,000, then the pattern cut to 200.

## nginx-access: `access.log`
```text
192.0.2.10 - - [15/Jan/2026:10:04:12 +0100] "GET /catalog?page=2 HTTP/1.1" 200 512 "-" "curl/8.5.0"
192.0.2.10 - - [15/Jan/2026:10:04:13 -0130] "\x16\x03" 400 150 "-" "-"
192.0.2.10 - - [15/Jan/2026:10:04:14 +0000] "GET /health HTTP/1.1" 503
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T09:04:12Z | `info` | `GET /catalog 200` | `GET /catalog <n>` | `538e4c29e569` |
| 2 | 2026-01-15T11:34:13Z | `warning` | `- - 400` | `- - <n>` | `bfa0a90ef849` |
| 3 | 2026-01-15T10:04:14Z | `error` | `GET /health 503` | `GET /health <n>` | `3c46eed710c6` |

- Line 1: the offset is applied; the query string is cut.
- Line 2: a request that doesn't split into three parts; a negative offset with minutes.
- Line 3: the status is the last field on the line.

## nginx-error: `error.log`
```text
2026/01/15 10:04:12 [warn] 41#41: *7 an upstream response is buffered to a temporary file
2026/01/15 10:04:13 [emerg] 41#41: bind() to 0.0.0.0:80 failed (98: Address already in use)
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:04:12Z | `warning` | `an upstream response is buffered to a temporary file` | `an upstream response is buffered to a temporary file` | `3bd48ce6310b` |
| 2 | 2026-01-15T10:04:13Z | `critical` | `bind() to 0.0.0.0:80 failed (98: Address already in use)` | `bind() to <ip> failed (<n>: Address already in use)` | `83140bb4d9a8` |

- Line 1: `*7 ` is dropped from the message.
- Line 2: `emerg` is critical; an address with a port.

## php-fpm: `php.log`
```text
[15-Jan-2026 10:04:12] NOTICE: fpm is running, pid 38
[31-Feb-2026 10:04:13] WARNING: [pool www] seems busy
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:04:12Z | `info` | `fpm is running, pid 38` | `fpm is running, pid <n>` | `62b09c437369` |
| 2 | 2026-01-15T10:04:12Z | `warning` | `[pool www] seems busy` | `[pool www] seems busy` | `ceb290c77a90` |

- Line 2: 31 February isn't a date: no time of its own, the level still read.

## postgres: `postgres.log`
```text
2026-01-15 10:04:12.345 UTC [2231] DETAIL:  a detail line with nothing before it
2026-01-15 10:04:13.000 UTC [2231] ERROR:  deadlock detected
2026-01-15 10:04:13.000 UTC [2231] HINT:  See server log for query details.
2026-01-15 10:04:14.000 UTC [2232] LOG:  checkpoint complete
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:04:12Z | `unknown` | `a detail line with nothing before it` | `a detail line with nothing before it` | `2373ca066fb5` |
| 2 | 2026-01-15T10:04:13Z | `error` | `deadlock detected` | `deadlock detected` | `5025cead45e0` |
| 3 | 2026-01-15T10:04:13Z | `error` | `See server log for query details.` | `See server log for query details.` | `8bd7e7583a2f` |
| 4 | 2026-01-15T10:04:14Z | `info` | `checkpoint complete` | `checkpoint complete` | `2ccd5fdc5598` |

- Line 1: `DETAIL` on the first line has no line before it: `unknown`.
- Line 3: `HINT` takes the level of the line before it.

## redis: `redis.log`
```text
1:signal-handler (1768471452) Received SIGTERM scheduling shutdown...
1:M 15 Jan 2026 10:04:12.345 - Accepted 192.0.2.10:50000
1:M 15 Jan 2026 10:04:13.001 * Background saving started by pid 41
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:04:12Z | `unknown` | `1:signal-handler (1768471452) Received SIGTERM scheduling shutdown...` | `<n>:signal-handler (<hex>) Received SIGTERM scheduling shutdown...` | `8b846eee8129` |
| 2 | 2026-01-15T10:04:12Z | `debug` | `Accepted 192.0.2.10:50000` | `Accepted <ip>` | `6df934109198` |
| 3 | 2026-01-15T10:04:13Z | `info` | `Background saving started by pid 41` | `Background saving started by pid <n>` | `7bfaf9d86945` |

- Line 1: no redis shape; it borrows the first dated line's time, and its 10-digit number is hex before it is a number.
- Line 2: `-` is debug.

## syslog: `syslog.log`
```text
2026-01-15T10:04:12.5-05:30 myhost cron[7][8]: job started
2026-01-15T10:04:13.000000+00:00 myhost sshd[812]: Connection closed by 192.0.2.10 port 50122
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T15:34:12Z | `unknown` | `cron[7]: job started` | `cron[<n>]: job started` | `775320fb57ef` |
| 2 | 2026-01-15T10:04:13Z | `unknown` | `sshd: Connection closed by 192.0.2.10 port 50122` | `sshd: Connection closed by <ip> port <n>` | `504577a36b8a` |

- Line 1: only the trailing `[8]` is removed; a one-digit fraction and a `-05:30` offset.

## pytest: `pytest.log`
```text
=== run 2026-01-15T10:00Z ===
tests/test_cart.py::test_add PASSED
FAILED tests/test_cart.py::test_remove PASSED
   FAILED tests/test_cart.py::test_indented - not a result at the start
tests/test_cart.py::test_skip SKIPPED (no network)
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | 2026-01-15T10:00:00Z | `info` | `=== run 2026-01-15T10:00Z ===` | `=== run <n>-<n>-<n>T<n>:<n>Z ===` | `324d505f63ef` |
| 2 | 2026-01-15T10:00:00Z | `info` | `tests/test_cart.py::test_add PASSED` | `tests/test_cart.py::test_add PASSED` | `e71a9fe27029` |
| 3 | 2026-01-15T10:00:00Z | `error` | `FAILED tests/test_cart.py::test_remove PASSED` | `FAILED tests/test_cart.py::test_remove PASSED` | `59c0ddfa5eaa` |
| 4 | 2026-01-15T10:00:00Z | `unknown` | `FAILED tests/test_cart.py::test_indented - not a result at the start` | `FAILED tests/test_cart.py::test_indented - not a result at the start` | `b38cf9f35d29` |
| 5 | 2026-01-15T10:00:00Z | `info` | `tests/test_cart.py::test_skip SKIPPED (no network)` | `tests/test_cart.py::test_skip SKIPPED (no network)` | `3fda9464f343` |

- Line 1: a run header without seconds reads.
- Line 3: starts with `FAILED ` and has a pass result: error wins.
- Line 4: an indented `FAILED` isn't at the start, and there is no result after `::`.

## gotest: `gotest.log`
```text
=== run yesterday ===
=== RUN   TestCart
    --- FAIL: TestCart/add (0.01s)
    FAIL indented is not a result
ok  \texample.com/shop\t0.004s
```

| Line | Time | Level | Message | Pattern | group_id |
|---|---|---|---|---|---|
| 1 | null | `info` | `=== run yesterday ===` | `=== run yesterday ===` | `c8eb13dde41d` |
| 2 | null | `debug` | `=== RUN   TestCart` | `=== RUN TestCart` | `e35e78378e72` |
| 3 | null | `error` | `--- FAIL: TestCart/add (0.01s)` | `--- FAIL: TestCart/add (<n>s)` | `412f338effd7` |
| 4 | null | `unknown` | `FAIL indented is not a result` | `FAIL indented is not a result` | `9553fe86cc51` |
| 5 | null | `info` | `ok  \texample.com/shop\t0.004s` | `ok example.com/shop <n>s` | `6a3746714853` |

- Line 1: a run header whose time doesn't read: `info`, and no line in the file has a time.
- Line 3: the `--- FAIL:` marker may be indented.
- Line 4: a line starting `FAIL` counts only without leading spaces.
- Line 5: tabs become single spaces in the pattern.
