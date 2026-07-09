package workflow

import "testing"

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

	missing := TaskLedgerEntry{ID: "t", OrderingDependencies: []string{"nope"}}
	if taskReady(RunLedger{}, missing) {
		t.Error("ordering dep on a missing task must not be ready")
	}
}
