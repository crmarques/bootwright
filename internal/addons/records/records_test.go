package records

import "testing"

func TestHasFailedStep(t *testing.T) {
	ready := Record{Steps: map[string]StepRecord{
		"a": {Status: RecordStatusReady},
		"b": {Status: RecordStatusReady},
	}}
	if ready.HasFailedStep() {
		t.Fatal("a record whose steps are all ready must not report a failed step")
	}

	withFailed := Record{Steps: map[string]StepRecord{
		"a": {Status: RecordStatusReady},
		"b": {Status: RecordStatusFailed},
	}}
	if !withFailed.HasFailedStep() {
		t.Fatal("a record with a failed continue-step must report it so the ready-skip does not strand the failure")
	}

	if (Record{}).HasFailedStep() {
		t.Fatal("a record with no steps must not report a failed step")
	}
}
