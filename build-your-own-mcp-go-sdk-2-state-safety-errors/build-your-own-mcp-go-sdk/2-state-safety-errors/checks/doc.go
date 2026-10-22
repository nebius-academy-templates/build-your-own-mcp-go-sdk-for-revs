// Package checks holds the black-box checks of practice 2. They build the
// server in ../../solution, or run the command in LOG_TRIAGE_CMD (a JSON array),
// and talk to it only over MCP on stdio; one check reads the raw protocol lines
// itself. There is one file per build task, t2_scans_test.go to
// t7_checks_test.go; harness_test.go starts the servers, builds the temporary
// log roots and holds the shared checks.
package checks
