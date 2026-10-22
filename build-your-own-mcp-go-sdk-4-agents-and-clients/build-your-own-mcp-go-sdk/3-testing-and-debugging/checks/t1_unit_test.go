package checks

// Task 1: your unit tests for the formats and the grouping steps.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

const task1 = "Task 1 writes the unit tests; see tricky-cases.md."

// The server module compiles its own tests: go test -run '^$' builds every
// package with its test files and runs nothing.
func TestTheServerModuleBuildsItsTests(t *testing.T) {
	needOwnProject(t)
	files, _ := ownTestFiles(t)
	if len(files["unit"]) == 0 {
		t.Fatalf("solution/internal/logs/ has no *_test.go files. %s", task1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", "^$", "./...")
	cmd.Dir = serverDir
	cmd.Env = append(cmd.Environ(), "GOWORK=off", "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the server's tests must build (cd solution && go test -count=1 -run '^$' ./...): %v\n%s. %s",
			err, tail(strings.TrimSpace(string(out)), 25), task1)
	}
}

func TestUnitTestsRunAndPass(t *testing.T) {
	checkLayer(t, "unit", 4, task1)
}
