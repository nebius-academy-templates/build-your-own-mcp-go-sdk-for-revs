# Deploys failed 2026-09-07 when db-01 ran out of connection slots

- Severity: high
- Saved: 2026-09-20T19:34:26Z

Deploys failed from 03:05 to 03:49 UTC on 2026-09-07 because Postgres on db-01 had no free connection slots. The provisioners logged "deploy finalize failed" with SQLSTATE 53300 (244 failures across provisioner-1 and provisioner-2, first 03:05:06, last 03:49:52). Postgres logged 122 matching rejections between 03:05:06 and 03:49:25. portal-api hit the same limit once at 03:27:04. Deploys completed normally again from 03:50:10.

The first sign of trouble was at 03:04:51, when Postgres terminated five existing connections "due to administrator command". Postgres was not restarted: it had been up since 00:09 and there is no shutdown line around 03:04. The first slot rejection followed 15 seconds later.

The logs do not show who issued the administrator command, what was holding the slots, or what freed them. Postgres logs only the rejections. A search of 02:30 to 04:30 for max_connections, pool, idle in transaction and pg_terminate found only the one portal-api line.

The SSH activity on db-01 is not related. In the window there are only failed logins from 172.18.0.5 (admin at 02:50, postgres at 03:02 to 03:10), all closed before authentication. sshd restarted at 00:08:55 and the first accepted login is opuser at 08:32:10, so no SSH session existed at 03:04. The same source tried admin, root and opuser on 2026-09-06 and again through the rest of 2026-09-07, and it tried postgres on cache-01 at 06:31. That is a recurring background pattern, not tied to the incident. Separately, 172.18.0.5 failed as opuser on 2026-09-06 and later logged in as opuser at 08:32 on 2026-09-07. What that host is cannot be told from the logs and is worth confirming.

Open points: the provisioners logged 244 failures against 122 Postgres rejections, and this was not reconciled. The Postgres line stating a shutdown at 2026-09-11 sits in a log stamped 2026-09-07, so that timestamp is unreliable and was not used. ci-py-1 and ci-go-1 logged test failures at 02:00 with different errors, before the deploy problem, and were not investigated.

## Evidence

- `pods/provisioner-1/archive/app-2026-09-07.log.gz` line 1013: 03:04:49 last normal deploy completed before the outage (deploy_id=13357)
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 186: 00:09:01 database ready to accept connections; no shutdown or restart between here and line 187
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 187: 03:04:51 first of five 'terminating connection due to administrator command' lines (187 to 191)
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 192: 03:05:06 first 'remaining connection slots are reserved for roles with the SUPERUSER attribute'
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 313: 03:49:25 last slot rejection; nothing more until a checkpoint line at 12:09
- `pods/provisioner-1/archive/app-2026-09-07.log.gz` line 5335: 03:05:09 deploy finalize failed with pq 53300 (deploy_id=14525)
- `pods/provisioner-1/archive/app-2026-09-07.log.gz` line 1074: 03:49:52 last deploy finalize failed (deploy_id=13478)
- `pods/provisioner-1/archive/app-2026-09-07.log.gz` line 5396: 03:50:10 deploys complete again (deploy_id=14647)
- `pods/portal-api-1/archive/app-2026-09-07.log.gz` line 1973: 03:27:04 portal-api 'db pool exhausted' with the same slots error, the only such line in the window
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 16: 00:08:55 sshd started on db-01; no accepted login until line 26
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 18: 02:50:08 invalid user admin from 172.18.0.5, first SSH event in the window
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 23: 03:02:40 authentication failure for user postgres from 172.18.0.5
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 25: 03:10:28 connection closed preauth; last SSH event before the first accepted login
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 26: 08:32:10 first accepted login (opuser from 172.18.0.5), after deploys recovered
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 3: 2026-09-06 04:23 same source, invalid user admin: same pattern the day before
- `vms/db-01/archive/auth-2026-09-07.log.gz` line 13: 2026-09-06 14:17 authentication failure for opuser from 172.18.0.5, before the later successful opuser login
- `vms/cache-01/archive/auth-2026-09-07.log.gz` line 13: 06:31:51 same source tried invalid user postgres on cache-01
- `vms/db-01/archive/postgres-2026-09-07.log.gz` line 185: 'shut down at 2026-09-11' in a line stamped 2026-09-07, so the timestamp is unreliable and not used
