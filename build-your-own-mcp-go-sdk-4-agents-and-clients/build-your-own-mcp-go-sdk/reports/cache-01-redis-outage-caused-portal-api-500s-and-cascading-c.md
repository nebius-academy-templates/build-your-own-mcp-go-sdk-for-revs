# cache-01 Redis outage caused portal-api 500s and cascading CI signup fixture failures

- Severity: high
- Saved: 2026-09-17T06:55:56Z

Redis on cache-01 was shut down via an operator-issued command at 2026-09-02T14:05:08Z and stayed unreachable for roughly 1h41m, until write activity resumed around 15:46-15:47Z. No "Ready to accept connections" line was captured for the restart in this window, so the logs don't show exactly when or how it came back up; the only login attempt on cache-01 during the gap (SSH as user "postgres" from 172.18.0.5, 15:39-15:46) failed authentication and so was not the actor that restored the service.

While Redis was down, portal-api-1/-2 could not resolve/connect to cache-01:6379 and threw "unhandled error ... path=/api/auth/signup ... err=Error -2 connecting to cache-01:6379. Name or service not known" (862 occurrences, 14:12:42Z-~15:33Z, occasional "Error 111 ... Connection refused" variants consistent with the service flapping during teardown/startup). This surfaced to clients as HTTP 500 on POST /api/auth/signup at the edge, 874 occurrences between 14:04:48Z and 15:33:36Z.

The ci-py-1 pytest run that started at 2026-09-02T14:00:00Z (all its lines are stamped with that run-start time, not real request time) exercises the real deployed API via httpx. Its shared `new_customer` fixture calls POST /auth/signup and raises for status; during the outage this fixture received real 500 responses and failed setup for essentially every test that depends on it (e.g. test_add_valid_domain[api-gateway.net]), producing hundreds of near-identical setup errors in that single run. This is a real, infra-caused CI failure, not a bug in the individual tests.

Distinguished from background noise present at similar rates on both 2026-09-01 and 2026-09-02, and therefore unrelated to this incident: gotest FAIL entries under TestCertificateOrder*/TestProvisionAccount* are explicitly labeled "flaky: seeded failure" (a deliberate randomized test mechanism); pytest test_signup_username_variety failures assert on status 409 (not 500) and never reference cache/redis; provisioner "acme challenge validation timeout" / "certificate order status=502" and quota-exceeded warnings occur at similar volumes on both days.

What the logs don't show: who or what issued the redis shutdown or the eventual restart command, and why the failed "postgres" SSH attempt happened around the same time as the recovery (most likely coincidental/unrelated given it failed auth).

## Evidence

- `vms/cache-01/archive/redis-2026-09-02.log.gz` line 34: Redis receives 'User requested shutdown...' at 2026-09-02T14:05:08.880Z
- `vms/cache-01/archive/redis-2026-09-02.log.gz` line 37: Redis exits ('ready to exit, bye bye') at 14:05:12.048Z
- `vms/cache-01/archive/redis-2026-09-03.log.gz` line 33: Write activity resumes ('1000 changes in 60 seconds') at 02 Sep 2026 15:47:00 despite the '09-03' filename; archive rotation naming lags the actual day
- `vms/cache-01/archive/auth-2026-09-03.log.gz` line 6: Only login attempt on cache-01 during the gap: SSH as invalid user 'postgres' from 172.18.0.5, 15:39:05Z
- `vms/cache-01/archive/auth-2026-09-03.log.gz` line 8: That SSH attempt fails authentication at 15:39:06Z, so it did not restore the service
- `pods/portal-api-1/archive/app-2026-09-02.log.gz` line 4107: First 'unhandled error ... connecting to cache-01:6379. Name or service not known' for /api/auth/signup at 14:12:42Z
- `pods/portal-api-1/archive/app-2026-09-02.log.gz` line 4142: Variant 'Error 111 ... Connect_call_failed' (connection refused) during flapping
- `pods/edge-7c1a/archive/nginx-access-2026-09-02.log.gz` line 8220: First HTTP 500 on POST /api/auth/signup at 14:04:48Z, client python-httpx (the CI test client)
- `pods/ci-py-1/archive/pytest-2026-09-02.log.gz` line 4679: Shared new_customer fixture posts to /auth/signup and calls raise_for_status()
- `pods/ci-py-1/archive/pytest-2026-09-02.log.gz` line 4684: Fixture receives 'Response [500 Internal Server Error]' during the outage
- `pods/ci-py-1/archive/pytest-2026-09-02.log.gz` line 4721: httpx.HTTPStatusError: Server error 500 for http://edge/api/auth/signup, causing setup ERROR for test_add_valid_domain[api-gateway.net]
- `pods/ci-go-1/archive/gotest-2026-09-02.log.gz` line 6852: Unrelated background noise: gotest failure explicitly labeled 'flaky: seeded failure', occurs at similar rate on 09-01 and 09-02
- `pods/ci-py-1/archive/pytest-2026-09-02.log.gz` line 68: Unrelated background noise: test_signup_username_variety asserts on 409, no cache/redis reference
