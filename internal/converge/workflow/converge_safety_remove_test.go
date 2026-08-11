package workflow

import "testing"

func TestRemoveApplyTaskConvergeSafetyResetsToMissing(t *testing.T) {
	runsDir := t.TempDir()
	task := classifyTask("storage.demo", ApplyTaskKindStorageCluster, "demo")
	hash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, task, hash, ConvergeSafetyOwner)

	if class, err := classifyApplyTaskState(task, runsDir, "test"); err != nil || class != ConvergeSafetyMatch {
		t.Fatalf("precondition: recorded task should match, got %q err=%v", class, err)
	}

	if err := RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
		t.Fatalf("RemoveApplyTaskConvergeSafety: %v", err)
	}
	if class, err := classifyApplyTaskState(task, runsDir, "test"); err != nil || class != ConvergeSafetyMissing {
		t.Fatalf("after removal the task must classify missing, got %q err=%v", class, err)
	}
	if err := RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
		t.Fatalf("removing an absent record must be a no-op, got %v", err)
	}
}
