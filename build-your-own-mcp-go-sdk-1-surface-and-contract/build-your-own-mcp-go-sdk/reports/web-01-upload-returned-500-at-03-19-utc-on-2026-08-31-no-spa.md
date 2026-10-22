# web-01 upload returned 500 at 03:19 UTC on 2026-08-31: no space left on device

- Severity: medium
- Saved: 2026-09-20T19:40:38Z

At 03:19:12 UTC on 2026-08-31 a POST /upload on web-01 (client 172.18.0.12) got a 500. PHP failed to write in /var/www/customer-site/upload.php on line 14: file_put_contents() got errno=28, "No space left on device". php-fpm relayed the notice to nginx, which returned the error page. The same error appears nowhere else in the bundle, so it was a single failed request. Uploads from 172.18.0.10 succeed again from 05:48 on the same day, and web-01 logged no other request between 03:19:12 and 05:48. The logs do not show what filled the disk, when it filled, or when space came back. web-01 keeps no disk usage record in this bundle. nginx does buffer upload bodies under /var/www/customer-site/uploads/nginx_tmp on that host, so uploads and their temp files share the site's storage, but no line shows that they filled it.

## Evidence

- `vms/web-01/archive/nginx-access-2026-08-31.log.gz` line 1: POST /upload from 172.18.0.12 at 03:19:12 returned 500 (first line of the day, and the only 5xx)
- `vms/web-01/archive/nginx-error-2026-08-31.log.gz` line 1: FastCGI stderr: file_put_contents() write of 4096 bytes failed with errno=28 No space left on device, upload.php line 14, same request
- `vms/web-01/archive/php-fpm-2026-08-31.log.gz` line 6: php-fpm pool www child 32 reports the same errno=28 write failure at 03:19:12
- `vms/web-01/archive/nginx-access-2026-08-31.log.gz` line 2: Next request on web-01 is a successful POST /upload (200) at 05:48:00 from 172.18.0.10
- `vms/web-01/archive/nginx-error-2026-08-31.log.gz` line 2: nginx buffers upload bodies to /var/www/customer-site/uploads/nginx_tmp, the same tree the site writes into
