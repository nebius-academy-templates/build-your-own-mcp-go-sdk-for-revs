// Package checks holds the black-box checks of practice 1. They build the
// server in ../../solution, or run the command in LOG_TRIAGE_CMD (a JSON array),
// and talk to it only over MCP on stdio. There is one file per build task,
// t2_server_test.go to t7_save_finding_test.go; harness_test.go starts the
// servers and holds the shared checks.
package checks
