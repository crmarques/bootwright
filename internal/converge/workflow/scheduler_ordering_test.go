package workflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestTaskReadyHardVsOrderingDeps(t *testing.T) {
	ledgerWith := func(depStatus TaskStatus) RunLedger {
		return RunLedger{Tasks: []TaskLedgerEntry{{ID: "dep", Status: depStatus}}}
	}
	hard := TaskLedgerEntry{ID: "t", Dependencies: []string{"dep"}}
	success := TaskLedgerEntry{ID: "t", SuccessDependencies: []string{"dep"}}
	ordering := TaskLedgerEntry{ID: "t", OrderingDependencies: []string{"dep"}}

	cases := []struct {
		status       TaskStatus
		hardReady    bool
		successReady bool
		orderedReady bool
	}{
		{TaskStatusPending, false, false, false},
		{TaskStatusRunning, false, false, false},
		{TaskStatusOK, true, true, true},
		{TaskStatusSkipped, true, false, true},
		{TaskStatusFailed, false, false, true},
		{TaskStatusBlocked, false, false, true},
		{TaskStatusCancelled, false, false, true},
	}
	for _, tc := range cases {
		ledger := ledgerWith(tc.status)
		if got := taskReady(ledger, hard); got != tc.hardReady {
			t.Errorf("hard dep with status %q: taskReady=%v, want %v", tc.status, got, tc.hardReady)
		}
		if got := taskReady(ledger, success); got != tc.successReady {
			t.Errorf("success dep with status %q: taskReady=%v, want %v", tc.status, got, tc.successReady)
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

func TestTaskDependencyPoliciesCoverEveryLedgerDependencyField(t *testing.T) {
	want := map[string]taskDependencyPolicy{
		"Dependencies":         taskDependencyAllowsSkipped,
		"SuccessDependencies":  taskDependencyRequiresSuccess,
		"OrderingDependencies": taskDependencyRequiresTerminal,
	}
	typ := reflect.TypeOf(TaskLedgerEntry{})
	seen := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !strings.HasSuffix(field.Name, "Dependencies") {
			continue
		}
		if _, ok := want[field.Name]; !ok {
			t.Errorf("TaskLedgerEntry dependency field %q has no scheduler policy", field.Name)
		}
		seen[field.Name] = true
	}
	for field := range want {
		if !seen[field] {
			t.Errorf("scheduler dependency policy names missing TaskLedgerEntry field %q", field)
		}
	}

	refs := taskDependencyRefs(TaskLedgerEntry{
		Dependencies:         []string{"allows-skipped"},
		SuccessDependencies:  []string{"requires-success"},
		OrderingDependencies: []string{"requires-terminal"},
	})
	got := map[string]taskDependencyPolicy{}
	for _, ref := range refs {
		got[ref.id] = ref.policy
	}
	for field, policy := range want {
		id := map[string]string{
			"Dependencies":         "allows-skipped",
			"SuccessDependencies":  "requires-success",
			"OrderingDependencies": "requires-terminal",
		}[field]
		if got[id] != policy {
			t.Errorf("%s policy = %d, want %d", field, got[id], policy)
		}
	}
}
