package converge

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// The storage --override safety helpers key on the aggregated object kind
// ("StorageCluster"), not the lowercase task-kind constant. This regression test
// classifies a real structurally-drifted, owned StorageCluster and asserts the
// helpers actually match it — a kind-constant mismatch silently returned empty,
// disabling the Ceph OSD-wipe data-loss warning, the reconcilable-only zap
// suppression, and the --reclaim-devices ownership gate.
func TestStorageOverrideHelpersMatchClassifiedObjects(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	before := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	after := destroyResetState(v1alpha1.StorageCephDistributionIBM)

	beforeTasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), before)
	if err != nil {
		t.Fatalf("plan before: %v", err)
	}
	for _, task := range beforeTasks {
		switch task.Entry.Kind {
		case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark %s: %v", task.Entry.ID, err)
			}
		}
	}

	afterTasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), after)
	if err != nil {
		t.Fatalf("plan after: %v", err)
	}
	objects, err := workflow.ClassifyApplyObjects(afterTasks, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	if wiped := OverrideDestructiveStorageClusters(objects); len(wiped) == 0 {
		t.Fatal("OverrideDestructiveStorageClusters must name the structurally-drifted StorageCluster (kind-constant regression)")
	}
	if owned := OwnedStorageClusters(objects); len(owned) == 0 {
		t.Fatal("OwnedStorageClusters must name the recorded owned StorageCluster (kind-constant regression)")
	}
}
