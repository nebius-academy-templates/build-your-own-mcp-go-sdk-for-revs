// Package checks holds the black-box checks of practice 4. They build the
// server in ../../solution, or run the command in LOG_TRIAGE_CMD (a JSON array),
// start it with their own --http 127.0.0.1:<free port> and LOG_TRIAGE_TOKENS,
// and talk to it over HTTP: with go-sdk's client, and with raw requests where
// they check status codes and headers. The version and instructions checks use
// stdio. There is one file per build task: t1_http_test.go,
// t2_principals_test.go and t3_prompt_test.go.
package checks
