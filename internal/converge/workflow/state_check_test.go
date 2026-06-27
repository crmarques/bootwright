package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func stateCheckTask(id, kind, cluster, clusterKind string) ApplyTask {
	return ApplyTask{Entry: TaskLedgerEntry{ID: id, Kind: kind, Label: id, Cluster: cluster, ClusterKind: clusterKind}}
}

func saveStateCheckRecord(t *testing.T, runsDir string, task ApplyTask, hash, owner string) {
	t.Helper()
	record := ConvergeSafetyRecord{
		APIVersion:  ConvergeSafetyAPIVersion,
		ResourceID:  applyTaskSafetyResourceID(task),
		DesiredHash: hash,
		Owner:       ConvergeSafetyOwnerIdentity{Manager: owner},
		Status:      ConvergeSafetyStatusReconciled,
		UpdatedAt:   time.Unix(0, 0).UTC(),
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatalf("save converge safety record: %v", err)
	}
}

func saveSubObjectRecord(t *testing.T, runsDir, resourceID, hash, owner string) {
	t.Helper()
	record := ConvergeSafetyRecord{
		APIVersion:  ConvergeSafetyAPIVersion,
		ResourceID:  resourceID,
		DesiredHash: hash,
		Owner:       ConvergeSafetyOwnerIdentity{Manager: owner},
		Status:      ConvergeSafetyStatusReconciled,
		UpdatedAt:   time.Unix(0, 0).UTC(),
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatalf("save converge safety record: %v", err)
	}
}

func TestStateCheckClassifiesDriftAbsenceAndMatch(t *testing.T) {
	runsDir := t.TempDir()
	match := stateCheckTask("container-cluster.demo", "containerCluster", "demo", "container")
	drift := stateCheckTask("container-cluster.drifted", "containerCluster", "drifted", "container")
	absent := stateCheckTask("storage.gone", "storageCluster", "gone", "storage")

	matchHash, err := ApplyTaskDesiredHash(match)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, match, matchHash, ConvergeSafetyOwner)      // applied with current desired state
	saveStateCheckRecord(t, runsDir, drift, "sha256:stale", ConvergeSafetyOwner) // desired state changed since apply
	// absent: no record at all -> never applied

	report, err := StateCheck([]ApplyTask{match, drift, absent}, v1alpha1.State{}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("expected drift, report claims in sync")
	}

	byName := map[string]StateCheckRoot{}
	for _, root := range report.Roots {
		byName[root.Name] = root
	}

	if r := byName["demo"]; r.Absent || len(r.Resources) != 0 || r.Matched != 1 {
		t.Fatalf("demo should be in sync, got %+v", r)
	}
	if r := byName["drifted"]; r.Absent || len(r.Resources) != 1 || r.Resources[0].Classification != ConvergeSafetyDrift {
		t.Fatalf("drifted should report one drift, got %+v", r)
	}
	if r := byName["gone"]; !r.Absent || len(r.Resources) != 0 {
		t.Fatalf("gone should be a single absence, got %+v", r)
	}
}

func TestStateCheckGranularDriftMixedResources(t *testing.T) {
	runsDir := t.TempDir()
	// Four resources under one present root (container/demo): one matched, one
	// drifted, one foreign, one never applied. A present root must report the
	// out-of-sync resources granularly (not collapse to a single absence) and keep
	// the matched count distinct.
	matched := stateCheckTask("container-cluster.demo", "containerCluster", "demo", "container")
	drift := stateCheckTask("addon.demo.virt", "clusterAddon", "demo", "container")
	foreign := stateCheckTask("machineinfra.demo.node0", "machineInfra", "demo", "container")
	missing := stateCheckTask("storage-attach.demo", "storageAttachment", "demo", "container")

	matchHash, err := ApplyTaskDesiredHash(matched)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, matched, matchHash, ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, drift, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, foreign, "sha256:stale", "someone-else")
	// missing: no record -> never applied, but the root is otherwise present.

	report, err := StateCheck([]ApplyTask{matched, drift, foreign, missing}, v1alpha1.State{}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("a root with drift/foreign/missing resources must not be in sync")
	}
	if len(report.Roots) != 1 {
		t.Fatalf("expected one root, got %d: %+v", len(report.Roots), report.Roots)
	}
	root := report.Roots[0]
	if root.Name != "demo" || root.Absent {
		t.Fatalf("present root demo must report granularly, not as a single absence, got %+v", root)
	}
	if root.Total != 4 || root.Matched != 1 {
		t.Fatalf("expected total 4 matched 1, got total=%d matched=%d", root.Total, root.Matched)
	}
	if len(root.Resources) != 3 {
		t.Fatalf("expected 3 out-of-sync resources reported granularly, got %d: %+v", len(root.Resources), root.Resources)
	}
	counts := map[ConvergeSafetyClassification]int{}
	for _, res := range root.Resources {
		counts[res.Classification]++
	}
	if counts[ConvergeSafetyDrift] != 1 || counts[ConvergeSafetyForeign] != 1 || counts[ConvergeSafetyMissing] != 1 {
		t.Fatalf("expected one drift, one foreign, one missing reported granularly, got %+v", counts)
	}
}

func TestStateCheckForeignOwner(t *testing.T) {
	runsDir := t.TempDir()
	task := stateCheckTask("container-cluster.demo", "containerCluster", "demo", "container")
	hash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, task, hash, "someone-else")

	report, err := StateCheck([]ApplyTask{task}, v1alpha1.State{}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("foreign ownership should not be in sync")
	}
	root := report.Roots[0]
	if len(root.Resources) != 1 || root.Resources[0].Classification != ConvergeSafetyForeign {
		t.Fatalf("expected foreign classification, got %+v", root)
	}
}

// TestStateCheckSharedResourceKeyTasksMatch covers F2/F14: distinct tasks that
// share an apply-time ResourceKey (the scheduler mutual-exclusion lock) must each
// keep their own converge-safety record. A provider task and a machine-infra
// finalize task on one host both carry "host:bastion:mutating"; recorded with
// their own desired hashes after a clean apply, state-check must report both as
// matched, not drift. With the record key derived from ResourceKeys, the two
// collided on one file and this reported drift.
func TestStateCheckSharedResourceKeyTasksMatch(t *testing.T) {
	runsDir := t.TempDir()
	shared := []string{"host:bastion:mutating"}
	provider := stateCheckTask("provider.bastion", "machineInfra", "demo", "container")
	provider.Entry.ResourceKeys = shared
	finalize := stateCheckTask("infrafinalize.demo.bastion", "machineInfra", "demo", "container")
	finalize.Entry.ResourceKeys = shared

	for _, task := range []ApplyTask{provider, finalize} {
		hash, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		saveStateCheckRecord(t, runsDir, task, hash, ConvergeSafetyOwner)
	}

	report, err := StateCheck([]ApplyTask{provider, finalize}, v1alpha1.State{}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if !report.InSync {
		t.Fatalf("shared-ResourceKey tasks recorded after a clean apply must be in sync, got %+v", report.Roots)
	}
}

// storageSubObjectTestState builds a managed StorageCluster with two pools and
// one export, all referencing it, for the sub-object granularity tests.
func storageSubObjectTestState(cluster string) v1alpha1.State {
	ref := v1alpha1.LocalObjectReference{Name: cluster}
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: cluster}}},
		StoragePools: []v1alpha1.StoragePool{
			{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: v1alpha1.StoragePoolSpec{StorageClusterRef: ref}},
			{Metadata: v1alpha1.Metadata{Name: "cephfs-data"}, Spec: v1alpha1.StoragePoolSpec{StorageClusterRef: ref}},
		},
		StorageExports: []v1alpha1.StorageExport{
			{Metadata: v1alpha1.Metadata{Name: "odf"}, Spec: v1alpha1.StorageExportSpec{StorageClusterRef: ref}},
		},
	}
}

// TestStateCheckGranularStorageSubObjectDrift covers the storage-granularity gap:
// on a present (applied) StorageCluster, state-check must name the specific pool
// or export that drifted or is missing, not collapse the whole cluster to one
// line. Sub-objects carry their own converge-safety records, so the report reads
// them independently of the cluster task.
func TestStateCheckGranularStorageSubObjectDrift(t *testing.T) {
	runsDir := t.TempDir()
	const cluster = "ceph"
	state := storageSubObjectTestState(cluster)

	clusterTask := stateCheckTask("storage."+cluster, "storageCluster", cluster, "storage")
	clusterHash, err := ApplyTaskDesiredHash(clusterTask)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, clusterTask, clusterHash, ConvergeSafetyOwner) // cluster applied, matches

	rbd := storageSubObject{storageSubObjectKindPool, cluster, "rbd"}
	rbdHash, err := storageSubObjectDesiredHash(state, rbd)
	if err != nil {
		t.Fatalf("sub-object desired hash: %v", err)
	}
	saveSubObjectRecord(t, runsDir, rbd.resourceID(), rbdHash, ConvergeSafetyOwner)                                                                       // pool rbd: match
	saveSubObjectRecord(t, runsDir, storageSubObject{storageSubObjectKindPool, cluster, "cephfs-data"}.resourceID(), "sha256:stale", ConvergeSafetyOwner) // pool cephfs-data: drift
	// export odf: no record -> missing

	report, err := StateCheck([]ApplyTask{clusterTask}, state, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("a storage cluster with a drifted pool and a missing export must not be in sync")
	}
	if len(report.Roots) != 1 {
		t.Fatalf("expected one storage root, got %d: %+v", len(report.Roots), report.Roots)
	}
	root := report.Roots[0]
	if root.Name != cluster || root.Absent {
		t.Fatalf("present storage cluster must report granularly, not as one absence, got %+v", root)
	}
	// 1 cluster task + 2 pools + 1 export = 4 resources; cluster and rbd matched.
	if root.Total != 4 || root.Matched != 2 {
		t.Fatalf("expected total 4 matched 2, got total=%d matched=%d", root.Total, root.Matched)
	}
	got := map[string]ConvergeSafetyClassification{}
	for _, res := range root.Resources {
		got[res.Label] = res.Classification
	}
	if len(root.Resources) != 2 {
		t.Fatalf("expected exactly 2 out-of-sync sub-objects, got %d: %+v", len(root.Resources), root.Resources)
	}
	if got["StoragePool/ceph.cephfs-data"] != ConvergeSafetyDrift {
		t.Fatalf("expected cephfs-data pool drift, got %+v", root.Resources)
	}
	if got["StorageExport/ceph.odf"] != ConvergeSafetyMissing {
		t.Fatalf("expected odf export missing, got %+v", root.Resources)
	}
	if _, reported := got["StoragePool/ceph.rbd"]; reported {
		t.Fatalf("matched rbd pool must not be reported, got %+v", root.Resources)
	}
}

// TestStateCheckStorageSubObjectsCollapseWhenClusterAbsent keeps the succinct-root-
// absence behavior: a never-applied StorageCluster (cluster task and every declared
// sub-object missing) reports one absence, not a per-pool/per-export flood.
func TestStateCheckStorageSubObjectsCollapseWhenClusterAbsent(t *testing.T) {
	runsDir := t.TempDir()
	const cluster = "ceph"
	state := storageSubObjectTestState(cluster)
	clusterTask := stateCheckTask("storage."+cluster, "storageCluster", cluster, "storage")
	// No records at all -> cluster task and every sub-object missing.

	report, err := StateCheck([]ApplyTask{clusterTask}, state, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("a never-applied storage cluster is not in sync")
	}
	if len(report.Roots) != 1 {
		t.Fatalf("expected one storage root, got %+v", report.Roots)
	}
	root := report.Roots[0]
	if !root.Absent || len(root.Resources) != 0 {
		t.Fatalf("a never-applied storage cluster must collapse to one absence even with declared sub-objects, got %+v", root)
	}
}
