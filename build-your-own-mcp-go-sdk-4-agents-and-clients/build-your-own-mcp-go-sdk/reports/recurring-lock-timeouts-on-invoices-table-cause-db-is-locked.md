# Recurring lock timeouts on invoices table cause DB is locked retries

- Severity: medium
- Saved: 2026-09-17T06:49:50Z

Since 2026-09-04, portal-api has experienced recurring PostgreSQL lock timeouts on db-01 when inserting into the invoices table (relation 16505, database 16387). A backend process waits on a RowExclusiveLock held by another backend PID; after ~5 minutes it fails with "canceling statement due to lock timeout". Portal-api retries the insert (logged as "DB is locked (retry N/3)") and the request always eventually succeeds, at the cost of 5-8+ seconds of extra latency.

Timing: the first sign is in the postgres log at 2026-09-04T09:35:45Z-09:40:34Z; the API-visible "DB is locked" errors start the next day, 2026-09-05T07:57:22Z. Frequency has been steady at roughly 2-3 episodes/day (6 "DB is locked" lines/day across the two portal-api pods) from 2026-09-05 through the most recent logs (2026-09-11), with no upward or downward trend. No request has ever exhausted all 3 configured retries ("retry 3/3" does not appear anywhere in pods/), so the issue degrades latency but has not caused outright request failures.

What the logs don't show: Postgres logs the PID holding the lock (e.g. "Process holding the lock: 683") but never logs that holder's own SQL statement, so the blocking transaction's query is not visible in these logs. I checked db-01's cron/syslog for a backup, vacuum, or pg_dump job that could align with the lock times - only @reboot and hourly run-parts cron entries exist, none matching. I also checked application logs across all pods for a second concurrent /api/invoices (or other) request at the same moments - none found. So the source of the blocking transaction cannot be identified from the available logs; it is most likely a long-running transaction in the same invoice-creation code path (e.g. holding the lock across an external call) whose SQL simply isn't logged.

Separately, db-01 also shows a one-time, unrelated burst of connection-pool exhaustion ("remaining connection slots are reserved for roles with the SUPERUSER attribute", 245 occurrences, and "terminating connection due to administrator command", 11 occurrences) confined to 2026-09-06T04:38Z-2026-09-07T03:49Z. Its time window does not overlap the recurring lock-timeout episodes, so it appears to be a distinct incident, not the cause of the lock contention.

## Evidence

- `vms/db-01/archive/postgres-2026-09-05.log.gz` line 14: Earliest lock event: process 36 waiting on RowExclusiveLock on relation 16505 (invoices), 2026-09-04T09:35:45Z
- `vms/db-01/archive/postgres-2026-09-05.log.gz` line 17: First 'canceling statement due to lock timeout' error, 2026-09-04T09:40:34Z
- `vms/db-01/postgres.log` line 15: DETAIL line names the holder PID (683) but never logs that PID's own statement
- `vms/db-01/postgres.log` line 16: Waiting statement is always INSERT INTO invoices (account_id, currency, total_cents) VALUES ($1,$2,$3) RETURNING id, status
- `vms/db-01/postgres.log` line 17: 2026-09-10T09:38:08Z lock timeout error, matching app-level retry seen the same morning
- `pods/portal-api-1/app.log.1` line 434: App-level 'DB is locked (retry 1/3) request_id=req-15af711f' at 2026-09-10T08:00:26Z
- `pods/portal-api-1/app.log.1` line 440: Same request (req-15af711f) finally succeeds at 08:05:19Z, duration_ms=5431 - ~5.4s added latency from the lock/retry cycle
- `pods/portal-api-1/archive/app-2026-09-05.log.gz` line 1971: First API-visible 'DB is locked' error, 2026-09-05T07:57:22Z, one day after the first DB-side lock event
- `vms/db-01/archive/syslog-2026-09-07.log.gz` line 5: Only cron activity on db-01 is @reboot and hourly run-parts; none align with the lock-timeout times, ruling out an obvious cron job as the blocker
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 20: Separate, unrelated burst: connection slots reserved for SUPERUSER, 245 occurrences confined to 2026-09-06T04:38Z-2026-09-07T03:49Z, does not overlap lock-timeout episodes
