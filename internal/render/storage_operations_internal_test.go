package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestCephFilesystemAttachesNonDefaultDataPools covers F26: `ceph fs new` wires
// only the default data pool, so every additional declared data pool must be
// attached with `ceph fs add_data_pool`, and the default pool must not be added
// a second time.
func TestCephFilesystemAttachesNonDefaultDataPools(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "fs1"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "fs1-meta"},
					DataPoolRefs: []v1alpha1.StorageCephFSDataPoolRef{
						{Name: "fs1-data-a", Default: true},
						{Name: "fs1-data-b"},
					},
				},
			},
		}},
	}

	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string][]string{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}

	if got := byName["create-cephfs-fs1"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "new", "fs1", "fs1-meta", "fs1-data-a"}) {
		t.Fatalf("create-cephfs command = %v", got)
	}
	if got := byName["add-cephfs-data-pool-fs1-fs1-data-b"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "add_data_pool", "fs1", "fs1-data-b"}) {
		t.Fatalf("add_data_pool command = %v", got)
	}
	if _, ok := byName["add-cephfs-data-pool-fs1-fs1-data-a"]; ok {
		t.Fatal("must not add_data_pool the default data pool")
	}
}

// The create-pool and create-cephfs operations carry the sub-object's immutable
// identity in a `structural` block, the only desired-state difference that warrants
// a data-destroying --override rebuild. Size/crush/application are NOT structural —
// they reconcile in place.
func TestStorageOperationsCarryStructuralIdentity(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePools: []v1alpha1.StoragePool{
			{
				Metadata: v1alpha1.Metadata{Name: "rbd"},
				Spec: v1alpha1.StoragePoolSpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated, Replicated: v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "ec"},
				Spec: v1alpha1.StoragePoolSpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeErasureCode, ErasureCoded: &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}},
				},
			},
		},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "fs1"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "fs1-meta"},
					DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "fs1-data", Default: true}},
				},
			},
		}},
	}
	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	structuralByName := map[string]map[string]any{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		if s, ok := op["structural"].(map[string]any); ok {
			structuralByName[name] = s
		}
	}
	if got := structuralByName["create-pool-rbd"]; got["type"] != v1alpha1.StoragePoolTypeReplicated {
		t.Fatalf("create-pool-rbd structural = %v, want type replicated", got)
	}
	ec := structuralByName["create-pool-ec"]
	if ec["type"] != v1alpha1.StoragePoolTypeErasureCode || ec["dataChunks"] != 2 || ec["codingChunks"] != 1 {
		t.Fatalf("create-pool-ec structural = %v, want erasure-coded 2+1", ec)
	}
	if got := structuralByName["create-cephfs-fs1"]; got["metadataPool"] != "fs1-meta" || got["defaultDataPool"] != "fs1-data" {
		t.Fatalf("create-cephfs-fs1 structural = %v, want metadataPool fs1-meta / defaultDataPool fs1-data", got)
	}
	// In-place ops must NOT carry structural (they reconcile, never destroy).
	if _, ok := structuralByName["set-pool-size-rbd"]; ok {
		t.Fatal("set-pool-size must not carry structural identity")
	}
}
