package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func storageSubObjectTestPool(name string, size int) v1alpha1.StoragePool {
	return v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
			Ceph: v1alpha1.StoragePoolCephSpec{
				Type:       v1alpha1.StoragePoolTypeReplicated,
				Replicated: v1alpha1.StorageCephPoolReplicas{Size: size, MinSize: size - 1},
			},
		},
	}
}

// Each sub-object is hashed against its own spec only: distinct pools hash
// differently, changing one pool never changes another's hash, and changing a pool's
// own spec changes its hash (drift). The record key is "<Kind>/<cluster>.<name>".
func TestStorageSubObjectDesiredHashIsolatesSubObjects(t *testing.T) {
	state := v1alpha1.State{StoragePools: []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 3), storageSubObjectTestPool("p2", 3)}}
	sub1 := storageSubObject{storageSubObjectKindPool, "demo", "p1"}
	sub2 := storageSubObject{storageSubObjectKindPool, "demo", "p2"}

	if got := sub1.resourceID(); got != "StoragePool/demo.p1" {
		t.Fatalf("resourceID = %q, want StoragePool/demo.p1", got)
	}
	h1 := mustSubHash(t, state, sub1)
	if h1 == mustSubHash(t, state, sub2) {
		t.Fatal("distinct pools must hash differently")
	}

	changed := v1alpha1.State{StoragePools: []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 3), storageSubObjectTestPool("p2", 2)}}
	if h1 != mustSubHash(t, changed, sub1) {
		t.Fatal("changing another pool must not change this pool's hash")
	}
	if mustSubHash(t, state, sub2) == mustSubHash(t, changed, sub2) {
		t.Fatal("changing a pool's own spec must change its hash (drift)")
	}
}

func mustSubHash(t *testing.T, state v1alpha1.State, sub storageSubObject) string {
	t.Helper()
	h, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	return h
}

// The user's worked example: a converged 4-pool cluster gains a 5th pool. The cluster
// and pools 1-4 stay match (so bootstrap and those pools are skipped); only pool 5
// reads missing (and is created). The cluster does not inherit the new pool's absence.
func TestClassifyApplyObjectsExpandsStorageSubObjects(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	pool := func(name string) v1alpha1.StoragePool { return storageSubObjectTestPool(name, 3) }
	base := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools:    []v1alpha1.StoragePool{pool("p1"), pool("p2"), pool("p3"), pool("p4")},
	}
	baseTask := ApplyTask{
		Entry:           TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
		State:           base,
		DesiredHashVars: storageClusterDesiredHashVars(base, "demo"),
	}
	if err := MarkApplyTaskConvergeSafety(runsDir, "", "", baseTask, ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark cluster: %v", err)
	}
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, "", "", base, "demo", ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark sub-objects: %v", err)
	}

	grown := base
	grown.StoragePools = append(append([]v1alpha1.StoragePool{}, base.StoragePools...), pool("p5"))
	grownTask := ApplyTask{
		Entry:           TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
		State:           grown,
		DesiredHashVars: storageClusterDesiredHashVars(grown, "demo"),
	}
	objs, err := ClassifyApplyObjects([]ApplyTask{grownTask}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	got := map[string]ConvergeSafetyClassification{}
	for _, o := range objs {
		got[o.ObjectKey] = o.Class
	}
	want := map[string]ConvergeSafetyClassification{
		"StorageCluster/demo": ConvergeSafetyMatch,
		"StoragePool/demo.p1": ConvergeSafetyMatch,
		"StoragePool/demo.p2": ConvergeSafetyMatch,
		"StoragePool/demo.p3": ConvergeSafetyMatch,
		"StoragePool/demo.p4": ConvergeSafetyMatch,
		"StoragePool/demo.p5": ConvergeSafetyMissing,
	}
	for key, wantClass := range want {
		if got[key] != wantClass {
			t.Errorf("%s = %q, want %q", key, got[key], wantClass)
		}
	}
}

// Editing an existing pool's size drifts only that pool; the cluster and other pools
// stay match. --continue would FAIL on this drift; --override rebuilds only this pool.
func TestClassifyApplyObjectsReportsSubObjectDrift(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	base := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools:    []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 3), storageSubObjectTestPool("p2", 3)},
	}
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, "", "", base, "demo", ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark sub-objects: %v", err)
	}
	drifted := v1alpha1.State{
		StorageClusters: base.StorageClusters,
		StoragePools:    []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 2), storageSubObjectTestPool("p2", 3)},
	}
	task := ApplyTask{
		Entry:           TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
		State:           drifted,
		DesiredHashVars: storageClusterDesiredHashVars(drifted, "demo"),
	}
	objs, err := ClassifyApplyObjects([]ApplyTask{task}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	got := map[string]ConvergeSafetyClassification{}
	for _, o := range objs {
		got[o.ObjectKey] = o.Class
	}
	if got["StoragePool/demo.p1"] != ConvergeSafetyDrift {
		t.Errorf("p1 = %q, want drift", got["StoragePool/demo.p1"])
	}
	if got["StoragePool/demo.p2"] != ConvergeSafetyMatch {
		t.Errorf("p2 = %q, want match", got["StoragePool/demo.p2"])
	}
}

// Destroy must reset sub-object records so a torn-down pool reclassifies as missing
// and a later apply recreates it, rather than a stale record reporting match.
func TestRemoveStorageSubObjectsConvergeSafetyResetsRecords(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools:    []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 3)},
	}
	sub := storageSubObject{storageSubObjectKindPool, "demo", "p1"}
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, "", "", state, "demo", ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, found, _ := LoadConvergeSafetyRecord(runsDir, sub.resourceID()); !found {
		t.Fatal("expected a record after marking")
	}
	if err := RemoveStorageSubObjectsConvergeSafety(runsDir, state, "demo"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, found, _ := LoadConvergeSafetyRecord(runsDir, sub.resourceID()); found {
		t.Fatal("record must be gone after destroy reset")
	}
	class, err := classifyStorageSubObject(state, sub, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if class != ConvergeSafetyMissing {
		t.Fatalf("class after reset = %q, want missing", class)
	}
}
