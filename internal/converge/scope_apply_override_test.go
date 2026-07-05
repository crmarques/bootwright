package converge

import (
	"strings"
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

	// The positive rebuild-authorization token MUST name exactly the clusters the
	// data-loss warning lists — a single source of truth so the operator is never
	// warned about one set and wiped on another. Assert the token names are the
	// warned labels with the "StorageCluster/" prefix stripped.
	authorized := RebuildAuthorizedStorageClusters(objects)
	if len(authorized) == 0 {
		t.Fatal("RebuildAuthorizedStorageClusters must positively authorize the structurally-drifted StorageCluster's --override wipe")
	}
	wiped := OverrideDestructiveStorageClusters(objects)
	if len(authorized) != len(wiped) {
		t.Fatalf("rebuild-authorization token (%v) and data-loss warning (%v) must cover the same clusters", authorized, wiped)
	}
	for i, label := range wiped {
		if want := strings.TrimPrefix(label, "StorageCluster/"); authorized[i] != want {
			t.Fatalf("authorized[%d]=%q must equal warned name %q (single source of truth)", i, authorized[i], want)
		}
	}
}

// A healthy owned StorageCluster with NO desired drift must never be authorized for
// a destructive --override wipe: the positive rebuild-authorization token stays
// empty, so the seed role's rm-cluster --zap-osds gate (which requires membership)
// leaves the cluster's OSD data intact and reconciles it idempotently in place.
// This is the StorageCluster healthy-match skip — the parity of the ContainerCluster
// install-state healthy-skip that Ceph previously lacked (apply --override zapped a
// no-drift owned cluster because the gate was drift-keyed opt-out, not authorization
// opt-in).
func TestRebuildAuthorizationSkipsHealthyMatchStorageCluster(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	tasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Record every storage task as reconciled against the SAME desired state, so a
	// re-classification reads a clean match (no structural, no reconcilable drift).
	for _, task := range tasks {
		switch task.Entry.Kind {
		case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark %s: %v", task.Entry.ID, err)
			}
		}
	}
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	if authorized := RebuildAuthorizedStorageClusters(objects); len(authorized) != 0 {
		t.Fatalf("a no-drift owned StorageCluster must NOT be authorized for a wipe, got %v", authorized)
	}
	if wiped := OverrideDestructiveStorageClusters(objects); len(wiped) != 0 {
		t.Fatalf("a no-drift owned StorageCluster must not appear in the data-loss warning, got %v", wiped)
	}
}
