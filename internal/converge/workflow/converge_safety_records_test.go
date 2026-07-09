package workflow

import "testing"

func TestHasConvergeSafetyRecords(t *testing.T) {
	if HasConvergeSafetyRecords("") {
		t.Fatal("empty runsDir must report no records")
	}
	runsDir := t.TempDir()
	if HasConvergeSafetyRecords(runsDir) {
		t.Fatal("a context with no safety records must report none (state-check stays off the spine)")
	}
	record := ConvergeSafetyRecord{
		APIVersion: ConvergeSafetyAPIVersion,
		ResourceID: "StorageCluster/demo",
		Owner:      ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner},
		Status:     ConvergeSafetyStatusReconciled,
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatalf("save converge safety record: %v", err)
	}
	if !HasConvergeSafetyRecords(runsDir) {
		t.Fatal("a recorded apply must make HasConvergeSafetyRecords report true")
	}
}
