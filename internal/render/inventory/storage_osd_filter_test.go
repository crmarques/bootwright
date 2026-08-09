package inventory

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageOSDDeviceFiltersVarsPreservesEveryDynamicSelector(t *testing.T) {
	rotational := false
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
		Name:  "node-a",
		Roles: []string{v1alpha1.StorageCephRoleOSD},
		OSD: &v1alpha1.StorageCephNodeOSD{
			DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true, Model: "DATA", Limit: 2},
			DBDevices:   &v1alpha1.StorageCephDeviceSelection{Vendor: "FAST", Rotational: &rotational, Size: "100G:"},
			WALDevices:  &v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/disk/by-id/wal"}},
			FilterLogic: "OR",
		},
	}}}}

	got := storageOSDDeviceFiltersVars(cluster, cluster.Spec.Ceph.Topology.Nodes[0])
	want := []any{
		map[string]any{"role": "data", "filterLogic": "OR", "all": true, "model": "DATA", "limit": 2},
		map[string]any{"role": "db", "filterLogic": "OR", "vendor": "FAST", "rotational": false, "size": "100G:"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("storageOSDDeviceFiltersVars = %#v, want %#v", got, want)
	}
}

func TestStorageHostsVarsMarksOnlyUnboundedManagedAllForAutoReclaim(t *testing.T) {
	state := v1alpha1.State{}
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
		{Name: "all", MachineRef: v1alpha1.LocalObjectReference{Name: "all"}, Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}},
		{Name: "narrow", MachineRef: v1alpha1.LocalObjectReference{Name: "narrow"}, Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true, Model: "DATA"}}},
		{Name: "limited", MachineRef: v1alpha1.LocalObjectReference{Name: "limited"}, Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true, Limit: 1}}},
		{Name: "unmanaged", MachineRef: v1alpha1.LocalObjectReference{Name: "unmanaged"}, Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}, Unmanaged: true}},
	}}}

	hosts := storageHostsVars(state, cluster)
	for i, name := range []string{"all", "narrow", "limited", "unmanaged"} {
		host := hosts[i].(map[string]any)
		_, reclaim := host["osdReclaimAll"]
		if reclaim != (name == "all") {
			t.Errorf("host %s osdReclaimAll present = %v, want %v", name, reclaim, name == "all")
		}
	}
}

func TestStorageOSDDeviceFilterGuardClassifiesEverySelectionField(t *testing.T) {
	typ := reflect.TypeOf(v1alpha1.StorageCephDeviceSelection{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	want := []string{"Paths", "PathSpecs", "All", "Model", "Vendor", "Rotational", "Size", "Limit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StorageCephDeviceSelection fields = %v, want %v; classify every new selector as static or dynamic, render dynamic selectors into osdDeviceFilters, and extend both the filter matcher and unbounded-all predicate before shipping it", got, want)
	}
}
