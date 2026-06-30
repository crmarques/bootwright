package workflow

import "testing"

// TestTaskReadyHardVsOrderingDeps locks in the two dependency semantics the
// scheduler honors: a hard Dependency blocks until the dep is OK/Skipped, while
// an OrderingDependency only sequences — it waits until the dep is terminal (any
// outcome) and then lets the task run. The destroy chain relies on the ordering
// form so a failed stage no longer blocks the independent later stages.
func TestTaskReadyHardVsOrderingDeps(t *testing.T) {
	ledgerWith := func(depStatus TaskStatus) RunLedger {
		return RunLedger{Tasks: []TaskLedgerEntry{{ID: "dep", Status: depStatus}}}
	}
	hard := TaskLedgerEntry{ID: "t", Dependencies: []string{"dep"}}
	ordering := TaskLedgerEntry{ID: "t", OrderingDependencies: []string{"dep"}}

	cases := []struct {
		status       TaskStatus
		hardReady    bool
		orderedReady bool
	}{
		{TaskStatusPending, false, false},
		{TaskStatusRunning, false, false},
		{TaskStatusOK, true, true},
		{TaskStatusSkipped, true, true},
		// The crux: a FAILED/BLOCKED dep blocks a hard dependent but releases an
		// ordering dependent (it is terminal, so order is satisfied).
		{TaskStatusFailed, false, true},
		{TaskStatusBlocked, false, true},
		{TaskStatusCancelled, false, true},
	}
	for _, tc := range cases {
		ledger := ledgerWith(tc.status)
		if got := taskReady(ledger, hard); got != tc.hardReady {
			t.Errorf("hard dep with status %q: taskReady=%v, want %v", tc.status, got, tc.hardReady)
		}
		if got := taskReady(ledger, ordering); got != tc.orderedReady {
			t.Errorf("ordering dep with status %q: taskReady=%v, want %v", tc.status, got, tc.orderedReady)
		}
	}

	// A still-unknown ordering dep (not yet in the ledger) is not ready: the task
	// must wait for the dep to exist and reach terminal first.
	missing := TaskLedgerEntry{ID: "t", OrderingDependencies: []string{"nope"}}
	if taskReady(RunLedger{}, missing) {
		t.Error("ordering dep on a missing task must not be ready")
	}
}
