package workflow

import "testing"

// After a destroy removes an object's convergence record, the object must
// reclassify as missing so a later apply recreates it (greenfield no longer
// refuses it; --continue no longer skips a gone object as already-applied).
func TestRemoveApplyTaskConvergeSafetyResetsToMissing(t *testing.T) {
	runsDir := t.TempDir()
	task := classifyTask("storage.demo", ApplyTaskKindStorageCluster, "demo")
	hash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, task, hash, ConvergeSafetyOwner)

	if class, err := classifyApplyTaskState(task, runsDir); err != nil || class != ConvergeSafetyMatch {
		t.Fatalf("precondition: recorded task should match, got %q err=%v", class, err)
	}

	if err := RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
		t.Fatalf("RemoveApplyTaskConvergeSafety: %v", err)
	}
	if class, err := classifyApplyTaskState(task, runsDir); err != nil || class != ConvergeSafetyMissing {
		t.Fatalf("after removal the task must classify missing, got %q err=%v", class, err)
	}
	// Idempotent: removing an absent record is a no-op, not an error.
	if err := RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
		t.Fatalf("removing an absent record must be a no-op, got %v", err)
	}
}
