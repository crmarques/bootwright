package records

import "testing"

func TestHasFailedHook(t *testing.T) {
	ready := Record{Hooks: map[string]HookRecord{
		"a": {Status: RecordStatusReady},
		"b": {Status: RecordStatusReady},
	}}
	if ready.HasFailedHook() {
		t.Fatal("a record whose hooks are all ready must not report a failed hook")
	}

	withFailed := Record{Hooks: map[string]HookRecord{
		"a": {Status: RecordStatusReady},
		"b": {Status: RecordStatusFailed},
	}}
	if !withFailed.HasFailedHook() {
		t.Fatal("a record with a failed continue-hook must report it so the ready-skip does not strand the failure")
	}

	if (Record{}).HasFailedHook() {
		t.Fatal("a record with no hooks must not report a failed hook")
	}
}
