package checks

// Task 2: your protocol and flow tests, with go-sdk's in-memory transport.

import "testing"

const task2 = "Task 2 writes the protocol and flow tests; see CLAUDE.md."

func TestProtocolTestsRunAndPass(t *testing.T) {
	checkLayer(t, "protocol", 3, task2)
}

func TestFlowTestsRunAndPass(t *testing.T) {
	checkLayer(t, "flow", 1, task2)
}
