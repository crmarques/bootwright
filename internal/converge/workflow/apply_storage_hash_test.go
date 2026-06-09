package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// A StorageCluster's convergence hash must be cluster-scoped: adding a StoragePool
// (a child object) must not change it, so a 4-pool cluster that grows a 5th pool
// stays "match" at the cluster level while the new pool is the only thing that
// needs creating. This is the user's worked example.
func TestStorageClusterHashIgnoresSubObjects(t *testing.T) {
	pool := func(name string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec:     v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"}},
		}
	}
	base := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools:    []v1alpha1.StoragePool{pool("p1"), pool("p2"), pool("p3"), pool("p4")},
	}
	grown := base
	grown.StoragePools = append(append([]v1alpha1.StoragePool{}, base.StoragePools...), pool("p5"))

	hashFor := func(state v1alpha1.State) string {
		task := ApplyTask{
			Entry:           TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
			DesiredHashVars: storageClusterDesiredHashVars(state, "demo"),
		}
		h, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		return h
	}

	if hashFor(base) != hashFor(grown) {
		t.Fatal("adding a StoragePool must not change the StorageCluster convergence hash")
	}
	if proj := storageClusterDesiredHashVars(grown, "demo"); len(proj.StoragePools) != 0 || len(proj.StorageFilesystems) != 0 {
		t.Fatalf("cluster-level projection must strip sub-objects, got %d pools", len(proj.StoragePools))
	}
}
