package converge

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestInfraDestroyResetsClusterStageConvergeRecords(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
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
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply-before", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark %s: %v", task.Entry.ID, err)
			}
		}
	}
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster:   "ocp",
		Status:    workflow.ClusterInstallStatusInstalled,
		Phase:     workflow.ClusterInstallPhaseComplete,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed install record: %v", err)
	}

	afterTasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), after)
	if err != nil {
		t.Fatalf("plan after: %v", err)
	}
	objects, err := workflow.ClassifyApplyObjects(afterTasks, runsDir)
	if err != nil {
		t.Fatalf("classify before reset: %v", err)
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeContinue, objects); err == nil || !strings.Contains(err.Error(), "StorageCluster/ceph-ibm") {
		t.Fatalf("precondition: expected storage drift, got %v", err)
	}

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", InfraScope, after, nil, nil, nil, false, false)

	objects, err = workflow.ClassifyApplyObjects(afterTasks, runsDir)
	if err != nil {
		t.Fatalf("classify after reset: %v", err)
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeContinue, objects); err != nil {
		t.Fatalf("apply after infra destroy should rebuild missing objects, got %v", err)
	}
	if _, found, err := workflow.LoadClusterInstallRecord(clustersDir, "ocp"); err != nil || found {
		t.Fatalf("container install record must be cleared after infra destroy, found=%v err=%v", found, err)
	}
}

func TestResetConvergeRecordsKeepsPartiallyDestroyedStorageCluster(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	st := twoCephClustersState()

	tasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), st)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, task := range tasks {
		switch task.Entry.Kind {
		case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark task %s: %v", task.Entry.ID, err)
			}
		}
	}
	for _, name := range []string{"ceph-a", "ceph-b"} {
		if err := workflow.MarkStorageSubObjectsConvergeSafety(runsDir, "ctx", "apply", st, name, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("mark sub-objects %s: %v", name, err)
		}
	}

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, []string{"ceph-a"}, nil, false, false)

	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		t.Fatalf("classify after reset: %v", err)
	}
	if !objectRecorded(objects, "StorageCluster/ceph-a") {
		t.Fatalf("partially-destroyed StorageCluster/ceph-a must keep its convergence record")
	}
	if objectRecorded(objects, "StorageCluster/ceph-b") {
		t.Fatalf("fully-destroyed StorageCluster/ceph-b must have its convergence record reset to missing")
	}
	if !objectRecorded(objects, "StoragePool/ceph-a.rbd") {
		t.Fatalf("partially-destroyed cluster's StoragePool/ceph-a.rbd must keep its record")
	}
	if objectRecorded(objects, "StoragePool/ceph-b.rbd") {
		t.Fatalf("fully-destroyed cluster's StoragePool/ceph-b.rbd record must be reset to missing")
	}

	err = workflow.EvaluateApplyModePreflight(workflow.ApplyModeCreate, objects)
	if err == nil || !strings.Contains(err.Error(), "ceph-a") {
		t.Fatalf("apply --expect-new must refuse the partially-destroyed ceph-a, got %v", err)
	}
	if strings.Contains(err.Error(), "StorageCluster/ceph-b") {
		t.Fatalf("apply --expect-new must not refuse the fully-destroyed ceph-b, got %v", err)
	}
}

func TestFullDestroySweepsRecordsForUndeclaredObjects(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	st := twoCephClustersState()

	tasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), st)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, task := range tasks {
		switch task.Entry.Kind {
		case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark task %s: %v", task.Entry.ID, err)
			}
		}
	}
	orphan := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: "storage.ceph-gone", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-gone"}}
	if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", orphan, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark orphan: %v", err)
	}
	if !workflow.HasConvergeSafetyRecords(runsDir) {
		t.Fatal("precondition: records must exist")
	}

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, false, false)

	if workflow.HasConvergeSafetyRecords(runsDir) {
		t.Fatal("a full-estate destroy must leave no converge safety records, including for objects deleted from the desired state before the destroy")
	}
}

func TestDestroyKindForApplyTaskKindSeparatesStorageNodeAccess(t *testing.T) {
	if got := destroyKindForApplyTaskKind(workflow.ApplyTaskKindStorageNodeAccess); got != workflow.DestroyTaskKindStorageNodeAccess {
		t.Fatalf("storage node access apply kind maps to %q, want %q: the Storage clusters destroy step no longer revokes node access itself, so a converge record for the nodeaccess task must only be cleared once the dedicated Storage node access destroy step succeeds", got, workflow.DestroyTaskKindStorageNodeAccess)
	}
	for _, kind := range []string{workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster} {
		if got := destroyKindForApplyTaskKind(kind); got != workflow.DestroyTaskKindStorageCluster {
			t.Fatalf("apply kind %q maps to %q, want %q", kind, got, workflow.DestroyTaskKindStorageCluster)
		}
	}
}

func TestDestroyKindIncludedExpandsMachineInfraToStorageNodeAccess(t *testing.T) {
	include := destroyKindIncluded(map[string]bool{workflow.DestroyTaskKindMachineInfra: true})
	if !include(workflow.DestroyTaskKindStorageNodeAccess) {
		t.Fatal("a successful Machine infrastructure destroy wipes the machine, so it must also count as reverting storage node access")
	}
	includeStorageOnly := destroyKindIncluded(map[string]bool{workflow.DestroyTaskKindStorageCluster: true})
	if includeStorageOnly(workflow.DestroyTaskKindStorageNodeAccess) {
		t.Fatal("a successful Storage clusters destroy must not, by itself, imply storage node access was reverted -- that is now a separate destroy step that can fail independently")
	}
}

func objectRecorded(objects []workflow.ObjectClassification, key string) bool {
	for _, o := range objects {
		if o.ObjectKey == key {
			return o.Recorded()
		}
	}
	return false
}

func twoCephClustersState() v1alpha1.State {
	cluster := func(name string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph:       &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionOSS},
			},
		}
	}
	pool := func(clusterName string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: clusterName},
				Ceph:              v1alpha1.StoragePoolCephSpec{Role: "rbd"},
			},
		}
	}
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster("ceph-a"), cluster("ceph-b")},
		StoragePools:    []v1alpha1.StoragePool{pool("ceph-a"), pool("ceph-b")},
	}
}

func destroyResetState(distribution string) v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-ibm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Distribution: distribution,
				},
			},
		}},
	}
}
