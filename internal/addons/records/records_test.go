package records

import (
	"reflect"
	"testing"
)

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

func TestStepObservedDigestsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Record{
		Cluster:   "ocp",
		Extension: "data-foundation",
		Status:    RecordStatusFailed,
		Steps: map[string]StepRecord{
			"attach": {
				Lifecycle:       "operatorReady",
				Status:          RecordStatusFailed,
				ObservedDigests: map[string]string{"exporterScript": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
		},
	}
	if err := SaveRecord(dir, want); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	got, found, err := LoadRecord(dir, want.Cluster, want.Extension)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if !found {
		t.Fatal("LoadRecord did not find the saved record")
	}
	if !reflect.DeepEqual(got.Steps["attach"].ObservedDigests, want.Steps["attach"].ObservedDigests) {
		t.Fatalf("observed digests = %v, want %v", got.Steps["attach"].ObservedDigests, want.Steps["attach"].ObservedDigests)
	}
}
