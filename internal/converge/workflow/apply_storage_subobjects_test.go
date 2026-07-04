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

// A pool whose only change is a reconcilable-in-place field (replica size) drifts but
// classifies as RECONCILABLE, so continue proceeds and --override does not wipe. A
// change to the pool's immutable identity (type) stays STRUCTURAL. A record written
// before the structural hash existed falls back to structural (fail-safe).
func TestStorageSubObjectPoolSizeIsReconcilableTypeIsStructural(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "demo"}}
	stateWith := func(pool v1alpha1.StoragePool) v1alpha1.State {
		return v1alpha1.State{
			StorageClusters: []v1alpha1.StorageCluster{cluster},
			StoragePools:    []v1alpha1.StoragePool{pool},
		}
	}
	clusterTask := func(state v1alpha1.State) ApplyTask {
		return ApplyTask{
			Entry:           TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
			State:           state,
			DesiredHashVars: storageClusterDesiredHashVars(state, "demo"),
		}
	}
	classifyPool := func(t *testing.T, runsDir string, state v1alpha1.State) ObjectClassification {
		t.Helper()
		objs, err := ClassifyApplyObjects([]ApplyTask{clusterTask(state)}, runsDir)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		for _, o := range objs {
			if o.ObjectKey == "StoragePool/demo.p1" {
				return o
			}
		}
		t.Fatalf("pool object not classified")
		return ObjectClassification{}
	}

	// Baseline: replicated pool, size 3, recorded (writes desired + structural hash).
	base := stateWith(storageSubObjectTestPool("p1", 3))
	runsDir := t.TempDir()
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, "", "", base, "demo", ConvergeSafetyStatusReconciled, now); err != nil {
		t.Fatalf("mark sub-objects: %v", err)
	}

	// Size 3 -> 2: reconcilable drift (structural hash unchanged).
	sized := classifyPool(t, runsDir, stateWith(storageSubObjectTestPool("p1", 2)))
	if sized.Class != ConvergeSafetyDrift {
		t.Fatalf("size change should DISPLAY as drift, got %q", sized.Class)
	}
	if !sized.HasReconcilableDrift() || sized.HasStructuralDrift() {
		t.Fatalf("pool size change must be reconcilable, not structural: reconcilable=%v structural=%v", sized.HasReconcilableDrift(), sized.HasStructuralDrift())
	}
	if !sized.Reconcilable {
		t.Fatalf("Reconcilable flag must be set for a size-only pool edit")
	}

	// Type replicated -> erasure: structural (immutable identity).
	ecPool := storageSubObjectTestPool("p1", 3)
	ecPool.Spec.Ceph.Type = v1alpha1.StoragePoolTypeErasureCode
	ecPool.Spec.Ceph.ErasureCoded = &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}
	typed := classifyPool(t, runsDir, stateWith(ecPool))
	if !typed.HasStructuralDrift() || typed.HasReconcilableDrift() {
		t.Fatalf("pool type change must be structural: structural=%v reconcilable=%v", typed.HasStructuralDrift(), typed.HasReconcilableDrift())
	}

	// Legacy record (no structural hash): a size edit falls back to structural.
	legacyDir := t.TempDir()
	desired, err := storageSubObjectDesiredHash(base, storageSubObject{storageSubObjectKindPool, "demo", "p1"})
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	legacy := ConvergeSafetyRecord{
		APIVersion:  ConvergeSafetyAPIVersion,
		ResourceID:  "StoragePool/demo.p1",
		DesiredHash: desired, // no StructuralHash
		Owner:       ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner},
		Status:      ConvergeSafetyStatusReconciled,
		UpdatedAt:   now.UTC(),
	}
	if err := SaveConvergeSafetyRecord(legacyDir, legacy); err != nil {
		t.Fatalf("save legacy record: %v", err)
	}
	legacySized := classifyPool(t, legacyDir, stateWith(storageSubObjectTestPool("p1", 2)))
	if !legacySized.HasStructuralDrift() || legacySized.HasReconcilableDrift() {
		t.Fatalf("legacy record must fall back to structural drift: structural=%v reconcilable=%v", legacySized.HasStructuralDrift(), legacySized.HasReconcilableDrift())
	}
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

// An NFS-Ganesha service added to a converged cluster follows the same worked
// example as the 5th pool: the cluster stays match (its hash excludes NFS
// exports) and only the new StorageNFSExport reads missing.
func TestClassifyApplyObjectsExpandsStorageNFSExports(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	base := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
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
	grown.StorageNFSExports = []v1alpha1.StorageNFSExport{{
		Metadata: v1alpha1.Metadata{Name: "shares"},
		Spec: v1alpha1.StorageNFSExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
			Ceph: v1alpha1.StorageNFSExportCephSpec{
				ServiceID: "shares",
				Placement: v1alpha1.StoragePlacement{Hosts: []string{"node-0"}},
			},
			Exports: []v1alpha1.StorageNFSExportEntry{{Pseudo: "/shares", FilesystemRef: v1alpha1.LocalObjectReference{Name: "fs"}}},
		},
	}}
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
	if got["StorageCluster/demo"] != ConvergeSafetyMatch {
		t.Errorf("cluster = %q, want match (NFS exports must not drift the cluster hash)", got["StorageCluster/demo"])
	}
	if got["StorageNFSExport/demo.shares"] != ConvergeSafetyMissing {
		t.Errorf("nfs export = %q, want missing", got["StorageNFSExport/demo.shares"])
	}
	if !IsStorageSubObjectKind("StorageNFSExport") {
		t.Error("StorageNFSExport must be an independently-classified sub-object kind")
	}
}

// Editing an existing pool's size drifts only that pool; the cluster and other pools
// stay match. A default apply would FAIL on this drift; --override rebuilds only this pool.
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
