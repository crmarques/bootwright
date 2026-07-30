package inventory

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageMonReadinessVarsCarriesShortDaemonNames(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{
			{Name: "node-02.ceph-prd.example.net", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
			{Name: "node-01.ceph-prd.example.net", Roles: []string{v1alpha1.StorageCephRoleMON}},
			{Name: "node-03.ceph-prd.example.net", Roles: []string{v1alpha1.StorageCephRoleOSD}},
		},
	}}

	want := []any{
		map[string]any{"name": "node-01.ceph-prd.example.net", "daemon": "node-01"},
		map[string]any{"name": "node-02.ceph-prd.example.net", "daemon": "node-02"},
	}
	if got := storageMonReadinessVars(cluster)["mons"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("mon readiness mons = %v, want %v", got, want)
	}
}

func TestStorageMonReadinessVarsIsEmptyWithoutMonRoles(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{{Name: "a", Roles: []string{v1alpha1.StorageCephRoleOSD}}},
	}}

	if got := storageMonReadinessVars(cluster)["mons"]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("mon readiness mons = %v, want empty", got)
	}
}
