package workflow

import (
	"strings"
	"testing"
)

func TestContinueDriftRefusalNamesConsequenceAndRemedy(t *testing.T) {
	runsDir := t.TempDir()
	osMachine := classifyTask("os.demo", ApplyTaskKindManagedMachineOS, "demo")
	storage := classifyTask("storage.demo", ApplyTaskKindStorageCluster, "demo")
	saveStateCheckRecord(t, runsDir, osMachine, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, storage, "sha256:stale", ConvergeSafetyOwner)

	objs, err := ClassifyApplyObjects([]ApplyTask{osMachine, storage}, runsDir, "test")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	err = EvaluateApplyModePreflight(ApplyModeReconcile, objs)
	if err == nil {
		t.Fatal("continue must refuse structural drift")
	}
	msg := err.Error()
	for _, want := range []string{
		"os.demo", "disks wiped",
		"StorageCluster/demo", "OSD data",
		"revert", "--mode rebuild",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal must contain %q: %v", want, msg)
		}
	}
}
