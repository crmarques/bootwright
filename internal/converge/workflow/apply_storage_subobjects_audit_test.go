package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func storageSubObjectTestNFSExport(name string) v1alpha1.StorageNFSExport {
	return v1alpha1.StorageNFSExport{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageNFSExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
			Ceph:              v1alpha1.StorageNFSExportCephSpec{ServiceID: "nfs"},
		},
	}
}

func TestStorageClusterHashIgnoresNFSExportSubObject(t *testing.T) {
	base := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
	}
	grown := base
	grown.StorageNFSExports = []v1alpha1.StorageNFSExport{storageSubObjectTestNFSExport("nfs-demo")}

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
		t.Fatal("adding a StorageNFSExport must not change the StorageCluster convergence hash")
	}
	if proj := storageClusterDesiredHashVars(grown, "demo"); len(proj.StorageNFSExports) != 0 {
		t.Fatalf("cluster-level projection must strip NFS exports, got %d", len(proj.StorageNFSExports))
	}

	subs := storageSubObjects(grown, "demo")
	var found bool
	for _, sub := range subs {
		if sub.Kind == storageSubObjectKindNFSExport && sub.Name == "nfs-demo" {
			found = true
			if got, want := sub.resourceID(), "StorageNFSExport/demo.nfs-demo"; got != want {
				t.Fatalf("resourceID = %q, want %q", got, want)
			}
		}
	}
	if !found {
		t.Fatalf("NFS export must enumerate as its own sub-object, got %+v", subs)
	}
	if !IsStorageSubObjectKind(storageSubObjectKindNFSExport) {
		t.Fatal("StorageNFSExport must classify as a storage sub-object kind")
	}
}

func TestStoragePoolHashTracksReferencedPlacementPolicy(t *testing.T) {
	pool := storageSubObjectTestPool("p1", 3)
	pool.Spec.PlacementPolicyRef = v1alpha1.LocalObjectReference{Name: "fast"}
	policy := func(size int) v1alpha1.StoragePlacementPolicy {
		return v1alpha1.StoragePlacementPolicy{
			Metadata: v1alpha1.Metadata{Name: "fast"},
			Spec: v1alpha1.StoragePlacementPolicySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Ceph:              v1alpha1.StoragePlacementCephSpec{Replicated: v1alpha1.StorageCephPoolReplicas{Size: size, MinSize: 2}},
			},
		}
	}
	sub := storageSubObject{storageSubObjectKindPool, "demo", "p1"}

	before := v1alpha1.State{
		StoragePools:             []v1alpha1.StoragePool{pool},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{policy(3)},
	}
	after := v1alpha1.State{
		StoragePools:             []v1alpha1.StoragePool{pool},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{policy(5)},
	}
	if mustSubHash(t, before, sub) == mustSubHash(t, after, sub) {
		t.Fatal("editing the referenced placement policy's replicated size must change the pool sub-object hash")
	}

	plainPool := storageSubObjectTestPool("p2", 3)
	plainSub := storageSubObject{storageSubObjectKindPool, "demo", "p2"}
	plainBefore := v1alpha1.State{
		StoragePools:             []v1alpha1.StoragePool{plainPool},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{policy(3)},
	}
	plainAfter := v1alpha1.State{
		StoragePools:             []v1alpha1.StoragePool{plainPool},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{policy(5)},
	}
	if mustSubHash(t, plainBefore, plainSub) != mustSubHash(t, plainAfter, plainSub) {
		t.Fatal("a pool without a placementPolicyRef must not track unrelated policy edits")
	}
}
