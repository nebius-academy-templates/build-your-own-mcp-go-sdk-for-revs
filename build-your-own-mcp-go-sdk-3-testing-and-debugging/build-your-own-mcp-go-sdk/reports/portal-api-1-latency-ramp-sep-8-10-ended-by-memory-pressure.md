# portal-api-1 latency ramp Sep 8-10 ended by memory-pressure restart

- Severity: medium
- Saved: 2026-09-20T19:48:56Z

portal-api-1 got steadily slower for about 70 hours, from Sep 8 to Sep 10 (UTC), then returned to normal latency at 22:00:54 on Sep 10. The service logged its own restart about four minutes later.

Trend (median duration_ms over all requests in pods/portal-api-1, from the timeline of a scan over that folder, 6-hour buckets): Sep 7 all day 0 ms; Sep 8 00:00-06:00 37 ms (fastest 30); Sep 8 12:00-18:00 67 ms; Sep 9 00:00-06:00 134 ms; Sep 9 12:00-18:00 210 ms; Sep 10 00:00-06:00 354 ms; Sep 10 12:00-18:00 506 ms (fastest 455); Sep 10 21:30-22:00 604 ms (fastest 596). The fastest request rises with the median, so every request slowed, not a few. Growth speeds up: about 2-3 ms/h on Sep 8, 11-15 ms/h on Sep 10. Logged request volume also fell, from about 3,900 lines per 6 hours on Sep 7 to 300-350 per 6 hours on Sep 10. The cause of the volume drop is not known.

What ended it: the last slow request is at 22:00:48 (608 ms). The next line, at 22:00:54, is a 28 ms signup, and requests stay at 0-28 ms after that. At 22:04:47 the service logs "memory pressure critical, restarting", and at 22:05:41 "portal-api starting". The edge logged one 502 on POST /api/auth/signup at 22:04:48 and an upstream-closed error in the same second. The upstream address (172.18.0.8) was not matched to this pod.

Open points: by timestamps, latency recovered about four minutes before the restart line, and the logs do not say what changed at 22:00:54. The restart message is also the only memory evidence in the pod; no RSS, heap or GC numbers are logged. A smooth, accelerating ramp that clears on restart fits a memory leak, but that is an inference, not something the logs show.

Unrelated to the ramp: requests of about 6.2 s appear from Sep 5 and continue after the restart (Sep 11 max 6220 ms); "DB is locked" errors (25, Sep 5-11) stay at about two per 6 hours through the ramp. Every archive starts with a "portal-api starting" line around 00:15-00:22; these were not investigated, and only the Sep 10 22:05 start has a stated reason.

## Evidence

- `pods/portal-api-1/archive/app-2026-09-08.log.gz` line 2: First slow request of the ramp: signup at 72 ms; lines use the 'request took Nms' format from here
- `pods/portal-api-1/app.log.1` line 1109: 21:30:36 signup at 629 ms, late in the ramp
- `pods/portal-api-1/app.log.1` line 1131: 22:00:48 last slow request, 608 ms
- `pods/portal-api-1/app.log.1` line 1132: 22:00:54 first fast request, signup at 28 ms; latency is back to normal from here
- `pods/portal-api-1/app.log.1` line 1341: 22:04:47 ERROR memory pressure critical, restarting
- `pods/portal-api-1/app.log.1` line 1342: 22:05:41 portal-api starting
- `pods/portal-api-1/app.log.1` line 1343: 22:10:19 next request logged after the restart
- `pods/edge-7c1a/nginx-access.log.1` line 2684: 22:04:48 502 on POST /api/auth/signup
- `pods/edge-7c1a/nginx-error.log.1` line 7: 22:04:48 upstream prematurely closed connection on POST /api/auth/signup
- `pods/edge-7c1a/nginx-error.log.1` line 8: 22:04:48 upstream server temporarily disabled
