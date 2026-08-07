package converge

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

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

	authorized := RebuildAuthorizedStorageClusters(objects)
	if len(authorized) == 0 {
		t.Fatal("RebuildAuthorizedStorageClusters must positively authorize the structurally-drifted StorageCluster's --mode rebuild wipe")
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

func TestSelectedCephStorageClustersCoverUnrecordedSelections(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{
		{Metadata: v1alpha1.Metadata{Name: "ceph-prd-01"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}},
		{Metadata: v1alpha1.Metadata{Name: "external"}},
	}}
	objects := []workflow.ObjectClassification{
		{Kind: workflow.ObjectKindStorageCluster, Label: "StorageCluster/ceph-prd-01"},
		{Kind: workflow.ObjectKindStorageCluster, Label: "StorageCluster/external"},
		{Kind: workflow.ObjectKindContainerCluster, Label: "ContainerCluster/ocp"},
	}
	if owned := OwnedStorageClusters(objects); len(owned) != 0 {
		t.Fatalf("objects without converge-safety records must classify as unowned (the post-destroy state), got %v", owned)
	}
	got := SelectedCephStorageClusters(state, objects)
	if len(got) != 1 || got[0] != "ceph-prd-01" {
		t.Fatalf("SelectedCephStorageClusters = %v, want only the selected Ceph cluster: a destroy releases ownership records, so this set is what --authorize unowned-devices lets the reclaim act on", got)
	}
}

func TestSubObjectRebuildAuthorizationKeysTrackStructuralDrift(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	before := twoCephClustersState()
	after := twoCephClustersState()
	after.StoragePools[0].Spec.Ceph.Type = v1alpha1.StoragePoolTypeErasureCode

	for _, cluster := range []string{"ceph-a", "ceph-b"} {
		if err := workflow.MarkStorageSubObjectsConvergeSafety(runsDir, "ctx", "apply", before, cluster, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("mark sub-objects of %s: %v", cluster, err)
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

	drifted := SubObjectRebuildAuthorizedKeys(objects)
	if len(drifted) != 1 || drifted[0] != "ceph-a/rbd" {
		t.Fatalf("SubObjectRebuildAuthorizedKeys must name exactly the structurally-drifted pool as <cluster>/<name>, got %v", drifted)
	}

	all := AllStorageSubObjectRebuildKeys(objects)
	joined := strings.Join(all, ",")
	if !strings.Contains(joined, "ceph-a/rbd") || !strings.Contains(joined, "ceph-b/rbd") {
		t.Fatalf("AllStorageSubObjectRebuildKeys must cover every selected cluster's sub-objects, got %v", all)
	}

	plan := &WorkflowPlan{}
	ApplySubObjectRebuildAuthorizedExtraVar(plan, drifted)
	if len(plan.ExtraVarPairs) != 1 || plan.ExtraVarPairs[0] != "bootwright_ceph_subobject_rebuild_authorized=ceph-a/rbd" {
		t.Fatalf("authorized sub-object extra var = %v, want bootwright_ceph_subobject_rebuild_authorized=ceph-a/rbd", plan.ExtraVarPairs)
	}

	empty := &WorkflowPlan{}
	ApplySubObjectRebuildAuthorizedExtraVar(empty, nil)
	if len(empty.ExtraVarPairs) != 0 {
		t.Fatalf("no authorized sub-objects must add no extra var, got %v", empty.ExtraVarPairs)
	}
}

func TestRebuildAuthorizationSkipsHealthyMatchStorageCluster(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	tasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
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
