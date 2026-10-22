# The log bundle in s3_logs

**Related**:

> [log-formats](log-formats.md): How to read every line in the bundle

`s3_logs/` holds fourteen days of logs from one small hosting platform: an edge proxy, two API instances, two provisioning workers, two CI runners, and three virtual machines for the web site, the database and the cache. The folder keeps the layout of the object store the logs were exported from. Your server reads it and never writes into it, and the checks and the practice questions rely on every byte staying as it is.

## Links
**Internal**:

- `docs/log-formats.md`
- `s3_logs/`

## Layout
```text
s3_logs/
  pods/
    ci-go-1/          CI runner, Go test runs        gotest
    ci-py-1/          CI runner, Python test runs    pytest
    edge-7c1a/        edge proxy                     nginx-access, nginx-error
    portal-api-1/     API instance                   app
    portal-api-2/     API instance                   app
    provisioner-1/    provisioning worker            app
    provisioner-2/    provisioning worker            app
  vms/
    cache-01/         cache server                   redis, syslog, auth
    db-01/            database server                postgres, syslog, auth
    web-01/           web server                     nginx-access, nginx-error, php-fpm, syslog, auth
```

The last column lists the streams in each folder. A stream's files are named after it, and its format comes from the content (see `docs/log-formats.md`). `auth` files are in the `syslog` format.

Every folder has the same three tiers for each stream, plus one file that isn't a log:

| File | Tier | Holds |
|---|---|---|
| `<stream>.log` | live | the current day |
| `<stream>.log.1` | rotated | the day before |
| `archive/<stream>-YYYY-MM-DD.log.gz` | archive | one earlier day, gzip-compressed |
| `host.json` | none | the machine's service name, environment and team; detected as `unknown` and skipped by scans |

## Calendar
All times in the logs are UTC.

| Tier | Days |
|---|---|
| archive | 2026-08-29 to 2026-09-09, one file per stream per day; `web-01` has no `nginx-error` archive for 2026-08-29 |
| `.log.1` | 2026-09-10 |
| `.log` | 2026-09-11, ending a few minutes into 2026-09-12 |

A file is named for the day it covers, but your server trusts the time written on each line, never the file name.

CI runners log several test runs a day. Each run opens with a header line, `=== run <time> ===`, and the lines of the run carry no time of their own.

## Sizes
| Fact | Value |
|---|---|
| Files | 275: 227 `.log.gz`, 19 `.log`, 19 `.log.1`, 10 `host.json` |
| On disk | 22 MB |
| Decompressed | 113 MB in 1,119,614 log lines |
| Largest file | under 4 MB decompressed |
| Longest line | 403 characters |

A question about the whole bundle makes your server read all 113 MB. Narrow the path or the time window when you can: a folder or a day is much cheaper than the root.
