package workflow

import (
	"strings"
	"testing"
)

// The continue-mode refusal states, per object, the worst-case consequence of a
// rebuild and the exact remedy (revert, or the --override path) — the "unmapped change
// = stop with guidance" contract. A managed-OS reinstall names disk wipe; a
// StorageCluster names the OSD wipe; both point at revert-or-override.
func TestContinueDriftRefusalNamesConsequenceAndRemedy(t *testing.T) {
	runsDir := t.TempDir()
	osMachine := classifyTask("os.demo", ApplyTaskKindManagedMachineOS, "demo")
	storage := classifyTask("storage.demo", ApplyTaskKindStorageCluster, "demo")
	saveStateCheckRecord(t, runsDir, osMachine, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, storage, "sha256:stale", ConvergeSafetyOwner)

	objs, err := ClassifyApplyObjects([]ApplyTask{osMachine, storage}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	err = EvaluateApplyModePreflight(ApplyModeContinue, objs)
	if err == nil {
		t.Fatal("continue must refuse structural drift")
	}
	msg := err.Error()
	for _, want := range []string{
		"os.demo", "disks wiped", // machine reinstall consequence
		"StorageCluster/demo", "OSD data", // Ceph wipe consequence
		"revert", "--override", // the remedy
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal must contain %q: %v", want, msg)
		}
	}
}
