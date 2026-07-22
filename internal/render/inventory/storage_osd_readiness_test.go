package inventory

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageOSDReadinessVarsRendersDynamicHosts(t *testing.T) {
	dynamic := v1alpha1.StorageCluster{}
	dynamic.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{
			{Name: "a", Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}},
			{Name: "b", Roles: []string{v1alpha1.StorageCephRoleOSD}},
		},
		OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
			ServiceID: "fleet",
			Placement: v1alpha1.StoragePlacement{Hosts: []string{"b"}},
			OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{Rotational: new(bool)}},
		}},
	}}

	got := storageOSDReadinessVars(dynamic)
	if got["mode"] != "atLeastOne" || got["expectedCount"] != 2 {
		t.Fatalf("dynamic readiness vars = %v", got)
	}
	if hosts := got["dynamicHosts"]; !reflect.DeepEqual(hosts, []string{"a", "b"}) {
		t.Fatalf("dynamic readiness hosts = %v, want [a b]", hosts)
	}

	static := v1alpha1.StorageCluster{}
	static.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
		Name:    "a",
		Roles:   []string{v1alpha1.StorageCephRoleOSD},
		Devices: []string{"/dev/sdb", "/dev/sdc"},
	}}}}
	got = storageOSDReadinessVars(static)
	if got["mode"] != "exact" || got["expectedCount"] != 2 {
		t.Fatalf("static readiness vars = %v", got)
	}
	if hosts := got["dynamicHosts"]; !reflect.DeepEqual(hosts, []string{}) {
		t.Fatalf("static readiness hosts = %v, want empty", hosts)
	}
}

func TestStorageOSDReadinessVarsRequiresTwoForDynamicSingleHost(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{
		Cephadm: v1alpha1.StorageCephadmSpec{
			Bootstrap: v1alpha1.StorageCephadmBootstrap{SingleHostDefaults: true},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
			Name:  "a",
			Roles: []string{v1alpha1.StorageCephRoleOSD},
			OSD: &v1alpha1.StorageCephNodeOSD{
				DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
			},
		}}},
	}

	got := storageOSDReadinessVars(cluster)
	if got["mode"] != "atLeastOne" || got["expectedCount"] != 2 {
		t.Fatalf("single-host dynamic readiness vars = %v", got)
	}
	if hosts := got["dynamicHosts"]; !reflect.DeepEqual(hosts, []string{"a"}) {
		t.Fatalf("single-host dynamic readiness hosts = %v, want [a]", hosts)
	}
}
