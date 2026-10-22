# Certificate order failures from ACME challenge validation timeouts (2026-09-01 to 2026-09-03)

- Severity: high
- Saved: 2026-09-17T06:55:54Z

Both provisioner replicas (provisioner-1 and provisioner-2) run a recurring certificate-issuance batch roughly every 6 hours (bursts around 02:0x, 08:0x, 14:0x, 20:0x UTC). Starting at 2026-09-01T14:01:03Z, a growing subset of certificate orders in each burst began failing with "acme challenge validation timeout domain=cert-N.example.test", immediately followed by the same request_id failing with "certificate order ... status=502". This is a real backend/dependency failure, not pod-specific: it affected both provisioner-1 and provisioner-2 identically and simultaneously, pointing to a shared downstream ACME challenge-validation dependency rather than the provisioner code itself.

Failure counts rose across each burst: 44, 53, 51, 49, 46, 64, 96, 108 (first burst on 09-01 14:01 through the last on 09-03 08:01). In the first burst alone, 44 of ~129 certificate-order attempts failed (~34%). After the 2026-09-03T08:01:12Z burst, the errors stop completely - the next burst (~09-03T14:00Z) and every burst through the most recent logs (2026-09-11) show zero acme-timeout errors.

The CI Go test suite (ci-go-1) independently corroborates this: its TestCertificateOrderStarter/Pro/Business/Enterprise tests run every 6 hours and normally show only 1-2 deliberately "seeded flaky" failures per run (marked "flaky: seeded failure ... certs_test.go:19"). Starting with the run stamped 2026-09-01T14:00:00Z, the suite instead shows dozens of genuine failures per run with a distinct message, "certs_test.go:30: expected certificate issuance to succeed, got 502" - a different code path than the seeded-flaky mechanism. These real 502 failures appear in every run from 2026-09-01T14:00Z through 2026-09-03T08:00Z (1013 total occurrences) and in none before or after, matching the provisioner incident window almost exactly.

What the logs don't show: no line in the provisioner, edge (pods/edge-7c1a has zero certificate/TLS-related lines in the whole window), or portal-api logs names why the ACME challenge validation itself timed out (no upstream error detail, no correlated DNS/network failure at the same timestamps). There is also no explicit "resolved"/config-change line explaining why the failures stopped after 2026-09-03T08:01 - they simply stop. A separate, likely unrelated redis_queue DNS lookup failure (cache-01 not found) occurs 2026-09-02T14:10-18:21 in the same provisioner pods, but its error signature is distinct from the ACME timeouts and only partially overlaps in time, so it is probably a coincidental separate issue rather than the root cause.

## Evidence

- `pods/provisioner-1/archive/app-2026-09-01.log.gz` line 3517: First occurrence: acme challenge validation timeout for domain cert-18.example.test at 2026-09-01T14:01:03.437Z
- `pods/provisioner-1/archive/app-2026-09-01.log.gz` line 3518: Same request_id immediately fails certificate order with status=502
- `pods/provisioner-1/archive/app-2026-09-03.log.gz` line 1399: Last burst of acme challenge validation timeouts begins at 2026-09-03T08:01:00.828Z
- `pods/provisioner-2/archive/app-2026-09-01.log.gz` line 3513: Same acme challenge timeout pattern on the second provisioner replica, confirming a shared dependency issue rather than a single-pod problem
- `pods/provisioner-1/archive/app-2026-09-02.log.gz` line 3243: Separate, likely unrelated redis_queue DNS lookup failure (cache-01) overlapping part of the same window
- `pods/edge-7c1a/nginx-error.log` line 1: Edge/ingress error log has zero certificate or TLS related lines in the entire window, so the issue is invisible at the edge layer
- `pods/ci-go-1/archive/gotest-2026-08-29.log.gz` line 883: Baseline: normal runs only show 1-2 deliberately seeded flaky failures per run, not genuine 502s
- `pods/ci-go-1/archive/gotest-2026-09-01.log.gz` line 10848: First genuine (non-seeded) CI failure: certs_test.go:30 expected certificate issuance to succeed, got 502, in the run stamped 2026-09-01T14:00:00Z
- `pods/ci-go-1/archive/gotest-2026-09-03.log.gz` line 18260: Last genuine CI failure of this kind, in the run stamped 2026-09-03T08:00:00Z; no such failures in later runs
