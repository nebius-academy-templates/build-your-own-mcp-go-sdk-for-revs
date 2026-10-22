# Hostile inputs

**Related**:

> [CONTRACT](CONTRACT.md): Paths, Files and Errors say what each input must answer
> [README](README.md): Task 6 builds the screening; the verify step uses this page

Inputs a careless or malicious caller can send, and what your server must answer. The checks for task 6 try each of them on a log root they build themselves. This page lets you try them by hand in the MCP Inspector.

## Links
- [MCP Inspector CLI](https://modelcontextprotocol.io/docs/2026-07-28/tools/inspector/cli)

**Internal**:

- `2-state-safety-errors/CONTRACT.md`
- `2-state-safety-errors/inspector.json`

## Build a hostile log root
`s3_logs/` has no symlinks, no broken archives and no huge files, so build a small log root that has them. Run this block from `2-state-safety-errors/` in bash or zsh on macOS or Linux. It creates a unique temporary folder and a matching Inspector config:

```bash
HOSTILE_PARENT=${TMPDIR:-/tmp}
HOSTILE_ROOT=$(mktemp -d "$HOSTILE_PARENT/log-triage-hostile.XXXXXX") || return 1
mkdir -p "$HOSTILE_ROOT/outside" "$HOSTILE_ROOT/reports" "$HOSTILE_ROOT/logs/walk" "$HOSTILE_ROOT/logs/broken" "$HOSTILE_ROOT/logs/cut" "$HOSTILE_ROOT/logs/big" "$HOSTILE_ROOT/logs/long"
printf '%s\n' '2026-01-15T09:00:00.000Z INFO vault outside the root, never served' > "$HOSTILE_ROOT/outside/secret.log"
printf '%s\n' '2026-01-15T10:00:00.000Z INFO api request served' '2026-01-15T10:00:01.000Z WARN api request slow' '2026-01-15T10:00:02.000Z ERROR api request failed' > "$HOSTILE_ROOT/logs/app.log"
ln -s ../outside/secret.log "$HOSTILE_ROOT/logs/link-out.log"
ln -s app.log "$HOSTILE_ROOT/logs/link-in.log"
ln -s ../outside "$HOSTILE_ROOT/logs/linkdir"
printf '%s\n' '2026-01-15T10:05:00.000Z INFO walker step one' '2026-01-15T10:05:01.000Z ERROR walker step two' > "$HOSTILE_ROOT/logs/walk/good.log"
ln -s ../app.log "$HOSTILE_ROOT/logs/walk/zz-link.log"
ln -s ../../outside "$HOSTILE_ROOT/logs/walk/sub-link"
printf 'this is not gzip data\n' > "$HOSTILE_ROOT/logs/broken/bad.log.gz"
printf '%s\n' '2026-01-15T10:06:00.000Z INFO mender still readable' > "$HOSTILE_ROOT/logs/broken/good.log"
seq 1 5000 | sed 's/.*/2026-01-15T10:07:00.000Z INFO copier chunk & copied/' | gzip -c > "$HOSTILE_ROOT/cut.gz"
head -c $(( $(wc -c < "$HOSTILE_ROOT/cut.gz") / 2 )) "$HOSTILE_ROOT/cut.gz" > "$HOSTILE_ROOT/logs/cut/cut.log.gz"
rm "$HOSTILE_ROOT/cut.gz"
yes '2026-01-15T10:00:00.000Z INFO filler padding line for the size cap' | head -n 1001625 | gzip -c > "$HOSTILE_ROOT/logs/big/huge.log.gz"
seq 1 60 | sed 's/.*/2026-01-15T10:08:00.000Z INFO filler sparse head line &/' > "$HOSTILE_ROOT/logs/big/sparse.log"
dd if=/dev/zero of="$HOSTILE_ROOT/logs/big/sparse.log" bs=1 count=1 seek=67108864 conv=notrunc 2>/dev/null
printf '%s\n' '2026-01-15T10:09:00.000Z INFO stretcher short line' > "$HOSTILE_ROOT/logs/long/long.log"
printf '2026-01-15T10:09:01.000Z INFO stretcher long line %s TAIL\n' "$(printf '%70000s' '' | tr ' ' x)" >> "$HOSTILE_ROOT/logs/long/long.log"
REPO_ROOT=$(cd .. && pwd -P)
HOSTILE_CONFIG="$HOSTILE_ROOT/inspector.json"
node -e 'const fs=require("fs"); const [repo,root,out]=process.argv.slice(1); const c=JSON.parse(fs.readFileSync("inspector.json","utf8")); c.mcpServers["log-triage-hostile"].args=["-C",repo+"/solution","run","./cmd/log-triage","--root",root+"/logs","--reports",root+"/reports"]; fs.writeFileSync(out,JSON.stringify(c,null,2)+"\n")' "$REPO_ROOT" "$HOSTILE_ROOT" "$HOSTILE_CONFIG"
printf 'Hostile root: %s\n' "$HOSTILE_ROOT"
```

What it makes under the folder printed by `printf '%s\n' "$HOSTILE_ROOT"`:

| Path | What it is |
|---|---|
| `outside/secret.log` | A log file outside the log root |
| `logs/` | The log root |
| `logs/app.log` | An ordinary file |
| `logs/link-out.log` | A symlink to the file outside |
| `logs/link-in.log` | A symlink to `app.log`, inside the root |
| `logs/linkdir` | A symlink to the folder outside |
| `logs/walk/` | An ordinary file next to a symlinked file (`zz-link.log`) and a symlinked folder (`sub-link`) |
| `logs/broken/bad.log.gz` | A `.gz` name on a file that isn't gzip, next to a good file |
| `logs/cut/cut.log.gz` | A gzip file cut in half: its first lines decode, the rest doesn't |
| `logs/big/huge.log.gz` | A small gzip file that decompresses to just over 64 MiB |
| `logs/big/sparse.log` | A plain file of just over 64 MiB that takes almost no disk space |
| `logs/long/long.log` | A file whose second line is 70,055 characters long |
| `reports/` | Where findings go, outside the log root |

The generated `$HOSTILE_CONFIG` gives `log-triage-hostile` absolute paths to this unique root and the repository's `solution/`. On macOS `/tmp` is itself a symlink: the root may sit under one, and your server must still serve it.

## Try the inputs
From this folder, one call per input:

```bash
npx @modelcontextprotocol/inspector --cli --config "$HOSTILE_CONFIG" --server log-triage-hostile --method tools/call --tool-name read_lines --tool-args-json '{"path": "link-out.log", "line": 1}' --format json
```

Change `--tool-name` and `--tool-args-json` for each row. A refused call prints its result on stdout, with the text to compare, and exits with code 5; stderr then shows `{"error":{"code":"tool_is_error",...}}`. `\u0000` inside the JSON is a NUL character.

| Input | Tool and arguments | The answer |
|---|---|---|
| A NUL byte | `list_logs` `{"path": "app.log\u0000"}` | E1 |
| An absolute path outside the root | `summarize_logs` `{"path": "/etc"}` | E1 |
| A path that climbs out | `list_logs` `{"path": "../"}` | E1 |
| A path that climbs out halfway | `search_logs` `{"path": "walk/../../outside/secret.log", "pattern": "."}` | E1 |
| A symlink to a file outside | `read_lines` `{"path": "link-out.log", "line": 1}` | E1 |
| A symlink to a file inside | `read_lines` `{"path": "link-in.log", "line": 1}` | E1 |
| A symlinked folder | `list_logs` `{"path": "linkdir"}` | E1 |
| A file through a symlinked folder | `read_lines` `{"path": "linkdir/secret.log", "line": 1}` | E1 |
| A path that isn't there | `list_logs` `{"path": "walk/missing.log"}` | E2 |
| A folder holding symlinks | `summarize_logs` `{"path": "walk", "min_level": "any"}` | `walk/sub-link` and `walk/zz-link.log` in `skipped`, reason `symlink`; one file scanned |
| The same folder, listed | `list_logs` `{"path": "walk"}` | only `walk/good.log` |
| A `.gz` file that isn't gzip | `summarize_logs` `{"path": "broken", "min_level": "any"}` | `broken/bad.log.gz` skipped, reason `unreadable`; the good file scanned |
| The same file, listed | `list_logs` `{"path": "broken"}` | `broken/bad.log.gz` with format `unknown` |
| The same file, read | `read_lines` `{"path": "broken/bad.log.gz", "line": 1}` | a masked error: no file system path, no exception text |
| A gzip file cut in half | `summarize_logs` `{"path": "cut", "min_level": "any"}` | skipped, reason `unreadable`; `lines_scanned` 0 |
| Files over 64 MiB | `summarize_logs` `{"path": "big", "min_level": "any"}` | both skipped, reason `too large` |
| A line over 65,536 characters | `search_logs` `{"path": "long", "pattern": "TAIL"}` | no match: the line was cut before its end |
| The same line, read | `read_lines` `{"path": "long/long.log", "line": 2}` | `text` cut to 500 characters |

Two more on the normal server, `log-triage`:

| Input | Tool and arguments | The answer |
|---|---|---|
| A folder where a file is needed | `read_lines` `{"path": "vms/web-01", "line": 1}` | E10 with `"vms/web-01"` |
| The root where a file is needed | `read_lines` `{"path": "", "line": 1}` | E10 with `"."` |

E1, E2 and E10 are the texts in `CONTRACT.md`, section 5. The error text quotes the path as you sent it (E1, E2) or as the server normalized it (E10), and never shows where the log root is on disk.

When you're done, remove the folder:

```bash
case "$HOSTILE_ROOT" in
  "$HOSTILE_PARENT"/log-triage-hostile.*) rm -r -- "$HOSTILE_ROOT" ;;
  *) printf 'Refusing to remove unexpected path: %s\n' "$HOSTILE_ROOT" >&2; return 1 ;;
esac
unset HOSTILE_CONFIG HOSTILE_PARENT HOSTILE_ROOT REPO_ROOT
```
