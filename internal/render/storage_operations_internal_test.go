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
