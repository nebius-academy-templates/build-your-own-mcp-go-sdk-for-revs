// Package checks holds the black-box checks of practice 3. They build the
// server in ../../solution, or run the command in LOG_TRIAGE_CMD (a JSON array),
// and talk to it only over MCP on stdio; the logging checks read its raw stdout
// and stderr. The checks for tasks 1 to 4 also run the server's own tests and
// its probe, and skip when LOG_TRIAGE_CMD is set. There is one file per build
// task: t1_unit_test.go, t2_protocol_test.go, t3_meaning_test.go,
// t4_probe_test.go, t6_logging_test.go and t7_timeline_test.go.
package checks
