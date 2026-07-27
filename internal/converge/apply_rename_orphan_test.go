package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func newContainerClusterObject(name string) workflow.ObjectClassification {
	return workflow.ObjectClassification{Kind: workflow.ObjectKindContainerCluster, Label: "ContainerCluster/" + name}
}

func stateWithClusters(names ...string) v1alpha1.State {
	var st v1alpha1.State
	for _, n := range names {
		st.ContainerClusters = append(st.ContainerClusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: n}})
	}
	return st
}

func TestCheckApplyRenameOrphan(t *testing.T) {
	dir := t.TempDir()
	if err := workflow.SaveClusterInstallRecord(dir, workflow.ClusterInstallRecord{
		Cluster: "prod-a", Status: workflow.ClusterInstallStatusInstalled, DesiredHash: "h",
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	created := []workflow.ObjectClassification{newContainerClusterObject("prod-east")}

	t.Run("rename co-occurrence refuses", func(t *testing.T) {
		err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir, nil)
		if err == nil {
			t.Fatal("a new cluster + an undeclared provisioned cluster must refuse as a possible rename")
		}
		msg := err.Error()
		for _, want := range []string{
			"temporarily restore the old cluster YAML (metadata.name prod-a)",
			"bootwright destroy --clusters prod-a",
			"destroy resolves --clusters against the declared state",
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("replace remedy must quote an executable sequence, missing %q: %s", want, msg)
			}
		}
	})
	t.Run("scoped apply refuses against the full declared state", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir, nil); err == nil {
			t.Fatal("a --clusters-scoped apply must still refuse when the full state no longer declares a provisioned cluster")
		}
	})
	t.Run("fully declared fleet is safe", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-a", "prod-east"), created, dir, nil); err != nil {
			t.Fatalf("a declared cluster is not an orphan: %v", err)
		}
	})
	t.Run("pure orphan with no new cluster is left alone", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters(), nil, dir, nil); err != nil {
			t.Fatalf("an undeclared cluster with no new cluster must not refuse (apply never touches it): %v", err)
		}
	})
}

func storageState(names ...string) v1alpha1.State {
	var st v1alpha1.State
	for _, n := range names {
		st.StorageClusters = append(st.StorageClusters, v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: n}})
	}
	return st
}

func storageOwnershipRecords(names ...string) []ownership.ResourceRecord {
	var out []ownership.ResourceRecord
	for _, n := range names {
		out = append(out, ownership.ResourceRecord{
			Kind:    string(ownership.KindStorageCluster),
			Name:    n,
			Owner:   ownership.Owner,
			Cluster: n,
		})
	}
	return out
}

func TestCheckApplyRenameOrphanCoversStorageClusters(t *testing.T) {
	dir := t.TempDir()
	created := []workflow.ObjectClassification{{Kind: workflow.ObjectKindStorageCluster, Label: workflow.ObjectKindStorageCluster + "/ceph-east"}}
	records := storageOwnershipRecords("ceph-storage")

	t.Run("renamed storage cluster refuses", func(t *testing.T) {
		err := CheckApplyRenameOrphan(storageState("ceph-east"), created, dir, records)
		if err == nil {
			t.Fatal("a new StorageCluster + an owned undeclared Ceph cluster must refuse as a possible rename")
		}
		for _, want := range []string{
			"StorageCluster(s) ceph-east",
			"orphan the old Ceph cluster with its OSD data",
			"bootwright destroy --clusters ceph-storage",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("storage rename refusal missing %q: %s", want, err)
			}
		}
	})
	t.Run("declared storage cluster is safe", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(storageState("ceph-storage", "ceph-east"), created, dir, records); err != nil {
			t.Fatalf("a declared Ceph cluster is not an orphan: %v", err)
		}
	})
	t.Run("orphan without a new storage cluster is left alone", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(storageState(), nil, dir, records); err != nil {
			t.Fatalf("an undeclared Ceph cluster with no new cluster must not refuse: %v", err)
		}
	})
	t.Run("foreign-owned record is not rename evidence", func(t *testing.T) {
		foreign := storageOwnershipRecords("ceph-storage")
		foreign[0].Owner = "someone-else"
		if err := CheckApplyRenameOrphan(storageState("ceph-east"), created, dir, foreign); err != nil {
			t.Fatalf("a non-Bootwright ownership record must not drive the rename gate: %v", err)
		}
	})
}
