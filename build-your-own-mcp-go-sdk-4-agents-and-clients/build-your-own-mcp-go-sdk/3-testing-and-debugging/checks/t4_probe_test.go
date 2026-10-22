package checks

// Task 4: the probe, a scripted stdio client, and its transcript (probe-expected.md).

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

const task4 = "Task 4 writes solution/cmd/probe; see probe-expected.md."

var (
	stepRE     = regexp.MustCompile(`^step (\d+) ([a-z-]+): ([a-z_]+) -> (.*)$`)
	probeSteps = []string{"out-of-order", "scan", "expired", "unknown-argument", "hostile-path"}
)

const scanPattern = `s_[0-9a-f]{16}`

type probeStep struct{ tool, outcome string }

func runProbeSteps(t *testing.T) map[string]probeStep {
	t.Helper()
	code, stdout, stderr := runProbe(t, s3Logs)
	if code != 0 {
		t.Fatalf("the probe must exit 0, it exited %d. %s\nstdout:\n%s\nstderr:\n%s",
			code, task4, lastBytes(stdout, 800), lastBytes(stderr, 800))
	}
	steps := map[string]probeStep{}
	var order []string
	for _, line := range strings.Split(stdout, "\n") {
		if m := stepRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if _, seen := steps[m[2]]; !seen {
				order = append(order, m[2])
			}
			steps[m[2]] = probeStep{m[3], m[4]}
		}
	}
	if !slices.Equal(order, probeSteps) {
		t.Fatalf("the probe must print the five steps of probe-expected.md in order, as "+
			"'step <k> <name>: <tool> -> <outcome>'; found %v.\nstdout:\n%s", order, lastBytes(stdout, 800))
	}
	return steps
}

func stepOutcome(t *testing.T, steps map[string]probeStep, name, tool string) string {
	t.Helper()
	step := steps[name]
	if step.tool != tool {
		t.Fatalf("probe step %s: the tool must be %s, got %s", name, tool, step.tool)
	}
	return step.outcome
}

func TestTheProbeRunsEveryStepInOrder(t *testing.T) {
	runProbeSteps(t)
}

func TestTheProbeTranscriptMatchesTheExpectedOutcomes(t *testing.T) {
	steps := runProbeSteps(t)
	first := stepOutcome(t, steps, "out-of-order", "group_lines")
	e6Shape := regexp.MustCompile("^tool error: " + strings.Replace(regexp.QuoteMeta(e6("ID")), "ID", "("+scanPattern+")", 1) + "$")
	if !e6Shape.MatchString(first) {
		t.Fatalf("step out-of-order: expected a tool error with E6 for a scan_id nobody made, got %q", first)
	}
	scan := regexp.MustCompile("^ok scan_id=(" + scanPattern + ")$").FindStringSubmatch(stepOutcome(t, steps, "scan", "summarize_logs"))
	if scan == nil {
		t.Fatalf("step scan: expected 'ok scan_id=<id>', got %q", steps["scan"].outcome)
	}
	if expired := stepOutcome(t, steps, "expired", "group_lines"); expired != "tool error: "+e6(scan[1]) {
		t.Fatalf("step expired: expected E6 for the scan of step 2 (%s), got %q", scan[1], expired)
	}
	if got := stepOutcome(t, steps, "unknown-argument", "list_logs"); got != "refused" {
		t.Fatalf("step unknown-argument: expected 'refused', got %q", got)
	}
	if hostile := stepOutcome(t, steps, "hostile-path", "read_lines"); hostile != "tool error: "+e1("../outside.log") {
		t.Fatalf("step hostile-path: got %q", hostile)
	}
}
