# invoice-errors: portal-api-2 500s from invoices-500 feature flag

- Severity: high
- Saved: 2026-09-17T06:59:51Z

Customers hitting the invoices page on portal-api-2 got 500 errors on every payment attempt (POST /api/invoices/{id}/pay) from 2026-09-09T11:00:02Z to 2026-09-09T11:40:05Z, about 40 minutes. The failure window is bracketed exactly by two admin calls to POST /admin/feature-flags/invoices-500, both returning 200: one at 11:00:02.922Z (flag turned on) and one at 11:40:05.690Z (flag turned off). Before and after this window, /pay calls succeeded with status 200. The other instance, portal-api-1, handled no /pay traffic at all during this window and was unaffected, so impact was scoped to portal-api-2. The app log only records a terse '500 on /api/invoices' error line with no stack trace or exception detail, so the exact internal code path that produced the 500 cannot be confirmed from these logs, only that it correlates with the flag being enabled. The admin feature-flag calls never appear in the edge nginx access or error logs, meaning they were made directly against the pod rather than through the public edge proxy; who or what triggered the toggle is not recorded (no actor/user field, only a request_id). The invoice IDs affected increment sequentially (e.g. 19590-19629 and a second interleaved sequence around 19941+), consistent with synthetic/test traffic (user-agent python-httpx) rather than organic customer load, and the flag name itself ('invoices-500') suggests a deliberate fault-injection/test toggle rather than an unplanned bug.

## Evidence

- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 1337: POST /admin/feature-flags/invoices-500 status=200 at 11:00:02.922Z - flag turned on, marks start of failure window
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 1336: Last successful /pay call (invoice 19589, status=200) just before the flag toggle at 10:59:36Z
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 1339: First 500 after the toggle: ERROR 500 on /api/invoices path=/api/invoices/19590/pay at 11:00:36Z
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 1419: Last 500 in the run, invoice 19629/pay at 11:39:11Z, just before the flag is turned off
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 1421: POST /admin/feature-flags/invoices-500 status=200 at 11:40:05.690Z - flag turned off, marks end of failure window
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 3536: Second, interleaved on-toggle at 11:00:01.910Z in the same file, followed by 500s on a different invoice ID sequence (19941+)
- `pods/portal-api-2/archive/app-2026-09-09.log.gz` line 3619: Second interleaved off-toggle at 11:40:03.971Z, matching the end of the second invoice ID sequence's failures
- `pods/edge-7c1a/archive/nginx-access-2026-09-09.log.gz` line 7081: Edge proxy confirms customer-facing 500: POST /api/invoices/19941/pay HTTP/1.1 500, corroborating the app-side error
- `pods/portal-api-1/app.log` line 1: portal-api-1 shows no /pay traffic in the 10:55-11:45 window (search returned zero matches), confirming the incident was scoped to portal-api-2 only
