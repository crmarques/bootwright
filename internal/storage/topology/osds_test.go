package topology

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func reclaimOSDHost(name string, roles, devices []string, osd *v1alpha1.StorageCephNodeOSD) v1alpha1.StorageCephNode {
	return v1alpha1.StorageCephNode{
		Name:       name,
		MachineRef: v1alpha1.LocalObjectReference{Name: name},
		Roles:      roles,
		Devices:    devices,
		OSD:        osd,
	}
}

func TestOSDHostUsesAllDevicesCoversOnlyAllTrue(t *testing.T) {
	truePtr := true
	sel := func(s v1alpha1.StorageCephDeviceSelection) *v1alpha1.StorageCephNodeOSD {
		return &v1alpha1.StorageCephNodeOSD{DataDevices: &s}
	}
	allHost := reclaimOSDHost("all-host", []string{v1alpha1.StorageCephRoleOSD}, nil, sel(v1alpha1.StorageCephDeviceSelection{All: true}))
	rotationalHost := reclaimOSDHost("rot-host", []string{v1alpha1.StorageCephRoleOSD}, nil, sel(v1alpha1.StorageCephDeviceSelection{Rotational: &truePtr}))
	modelHost := reclaimOSDHost("model-host", []string{v1alpha1.StorageCephRoleOSD}, nil, sel(v1alpha1.StorageCephDeviceSelection{Model: "SAMSUNG"}))
	sizeHost := reclaimOSDHost("size-host", []string{v1alpha1.StorageCephRoleOSD}, nil, sel(v1alpha1.StorageCephDeviceSelection{Size: "100G:"}))
	pathHost := reclaimOSDHost("path-host", []string{v1alpha1.StorageCephRoleOSD}, nil, sel(v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/sdb"}}))
	shorthandHost := reclaimOSDHost("sh-host", []string{v1alpha1.StorageCephRoleOSD}, []string{"/dev/sdc"}, nil)
	nonOSDHost := reclaimOSDHost("mon-host", []string{v1alpha1.StorageCephRoleMON}, nil, sel(v1alpha1.StorageCephDeviceSelection{All: true}))
	drivegroupHost := reclaimOSDHost("dg-host", []string{v1alpha1.StorageCephRoleOSD}, nil, nil)

	cluster := v1alpha1.StorageCluster{}
	cluster.Metadata.Name = "ceph"
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{allHost, rotationalHost, modelHost, sizeHost, pathHost, shorthandHost, nonOSDHost, drivegroupHost},
		OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
			ServiceID: "fleet",
			Placement: v1alpha1.StoragePlacement{Hosts: []string{"dg-host"}},
			OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}},
		}},
	}}

	cases := []struct {
		host v1alpha1.StorageCephNode
		want bool
	}{
		{allHost, true},
		{rotationalHost, false},
		{modelHost, false},
		{sizeHost, false},
		{pathHost, false},
		{shorthandHost, false},
		{nonOSDHost, false},
		{drivegroupHost, true},
	}
	for _, tc := range cases {
		if got := OSDHostUsesAllDevices(cluster, tc.host); got != tc.want {
			t.Errorf("OSDHostUsesAllDevices(%s) = %v, want %v", tc.host.Name, got, tc.want)
		}
	}
	if !ClusterHasAllDevicesOSDHost(cluster) {
		t.Error("ClusterHasAllDevicesOSDHost = false, want true (cluster has an all:true OSD host)")
	}

	narrowing := v1alpha1.StorageCluster{}
	narrowing.Metadata.Name = "narrow"
	narrowing.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{rotationalHost, modelHost, pathHost, shorthandHost},
	}}
	if ClusterHasAllDevicesOSDHost(narrowing) {
		t.Error("ClusterHasAllDevicesOSDHost(narrowing/static only) = true, want false")
	}
}
