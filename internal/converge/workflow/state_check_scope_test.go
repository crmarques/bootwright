package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func stateCheckScopeState() v1alpha1.State {
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph:       &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionOSS},
			},
		}},
		StoragePools: []v1alpha1.StoragePool{{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Role: "rbd"},
			},
		}},
	}
}

func storageRoot(report StateCheckReport) *StateCheckRoot {
	for i := range report.Roots {
		if report.Roots[i].Kind == ApplyClusterKindStorage {
			return &report.Roots[i]
		}
	}
	return nil
}

func TestStateCheckOmitsStorageWhenStagePlansNoStorageTask(t *testing.T) {
	runsDir := t.TempDir()
	state := stateCheckScopeState()

	report, err := StateCheck(nil, ApplyTarget{}, state, runsDir, "test")
	if err != nil {
		t.Fatalf("StateCheck (no storage task): %v", err)
	}
	if root := storageRoot(report); root != nil {
		t.Fatalf("state-check must not report a storage root when no StorageCluster task is planned, got %+v", root)
	}
	if !report.InSync {
		t.Fatalf("state-check must be in sync when the scope plans no storage, got %+v", report.Roots)
	}

	storageTask := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:          "storage.ceph",
			Kind:        ApplyTaskKindStorageCluster,
			Cluster:     "ceph",
			ClusterKind: ApplyClusterKindStorage,
			Label:       "StorageCluster/ceph",
		},
		State: state,
	}
	report, err = StateCheck([]ApplyTask{storageTask}, ApplyTarget{}, state, runsDir, "test")
	if err != nil {
		t.Fatalf("StateCheck (with storage task): %v", err)
	}
	root := storageRoot(report)
	if root == nil {
		t.Fatalf("state-check must report a storage root when a StorageCluster task is planned")
	}
	if !root.Absent {
		t.Fatalf("never-applied storage cluster must collapse to one absence, got %+v", root)
	}
	if report.InSync {
		t.Fatalf("state-check must report drift when the planned storage cluster is absent")
	}
}
