package ceph_test

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/ceph"
)

func osdHost(hostname string, roles []string, devices []string, osd *v1alpha1.StorageCephNodeOSD) v1alpha1.StorageCephNode {
	return v1alpha1.StorageCephNode{
		Name:       hostname,
		MachineRef: v1alpha1.LocalObjectReference{Name: hostname},
		Roles:      roles,
		Devices:    devices,
		OSD:        osd,
	}
}

func TestOSDGateDevicePathsCoversShorthandDrivegroupAndFleet(t *testing.T) {
	sel := func(paths ...string) *v1alpha1.StorageCephDeviceSelection {
		return &v1alpha1.StorageCephDeviceSelection{Paths: paths}
	}
	cluster := v1alpha1.StorageCluster{}
	cluster.Metadata.Name = "ceph"
	drivegroupHost := osdHost("dg-host", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
		DataDevices: sel("/dev/sdb", "/dev/sdc"),
		DBDevices:   sel("/dev/nvme0n1"),
	})
	fleetHost := osdHost("fleet-host", []string{"osd"}, nil, nil)
	shorthandHost := osdHost("sh-host", []string{"osd"}, []string{"/dev/sdd"}, nil)
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{drivegroupHost, fleetHost, shorthandHost},
		OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
			ServiceID: "fleet",
			Placement: v1alpha1.StoragePlacement{Hosts: []string{"fleet-host"}},
			OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: sel("/dev/sde")},
		}},
	}}

	cases := []struct {
		node v1alpha1.StorageCephNode
		want []string
	}{
		{drivegroupHost, []string{"/dev/sdb", "/dev/sdc", "/dev/nvme0n1"}},
		{fleetHost, []string{"/dev/sde"}},
		{shorthandHost, []string{"/dev/sdd"}},
	}
	for _, tc := range cases {
		got := ceph.OSDGateDevicePaths(cluster, tc.node)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("OSDGateDevicePaths(%s) = %v, want %v", tc.node.Name, got, tc.want)
		}
	}
}

func TestOSDGateDevicePathsOmitsFilterOnlySelection(t *testing.T) {
	cluster := v1alpha1.StorageCluster{}
	cluster.Metadata.Name = "ceph"
	filterHost := osdHost("filter-host", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
		DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
	})
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
		Nodes: []v1alpha1.StorageCephNode{filterHost},
	}}
	if got := ceph.OSDGateDevicePaths(cluster, filterHost); got != nil {
		t.Errorf("OSDGateDevicePaths(filter host) = %v, want nil (filter/all not statically enumerable)", got)
	}
}

func TestOSDReadinessExpectation(t *testing.T) {
	sel := func(paths ...string) *v1alpha1.StorageCephDeviceSelection {
		return &v1alpha1.StorageCephDeviceSelection{Paths: paths}
	}
	newCluster := func(hosts []v1alpha1.StorageCephNode, dgs []v1alpha1.StorageCephOSDDrivegroup) v1alpha1.StorageCluster {
		c := v1alpha1.StorageCluster{}
		c.Metadata.Name = "ceph"
		c.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: hosts, OSDDrivegroups: dgs}}
		return c
	}
	singleHostCluster := func(host v1alpha1.StorageCephNode) v1alpha1.StorageCluster {
		cluster := newCluster([]v1alpha1.StorageCephNode{host}, nil)
		cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults = true
		return cluster
	}
	cases := []struct {
		name             string
		cluster          v1alpha1.StorageCluster
		wantMode         string
		wantCount        int
		wantDynamicHosts []string
	}{
		{
			name:      "shorthand exact",
			cluster:   newCluster([]v1alpha1.StorageCephNode{osdHost("a", []string{"osd"}, []string{"/dev/sdb", "/dev/sdc"}, nil)}, nil),
			wantMode:  "exact",
			wantCount: 2,
		},
		{
			name: "explicit paths with osdsPerDevice",
			cluster: newCluster([]v1alpha1.StorageCephNode{osdHost("a", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
				DataDevices: sel("/dev/sdb"), OSDsPerDevice: 3,
			})}, nil),
			wantMode:  "exact",
			wantCount: 3,
		},
		{
			name: "filter selection is atLeastOne",
			cluster: newCluster([]v1alpha1.StorageCephNode{osdHost("a", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
				DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
			})}, nil),
			wantMode:         "atLeastOne",
			wantCount:        1,
			wantDynamicHosts: []string{"a"},
		},
		{
			name:      "single-host static selection requires two in OSDs",
			cluster:   singleHostCluster(osdHost("a", []string{"osd"}, []string{"/dev/sdb"}, nil)),
			wantMode:  "exact",
			wantCount: 2,
		},
		{
			name: "single-host dynamic selection requires two in OSDs",
			cluster: singleHostCluster(osdHost("a", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
				DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
			})),
			wantMode:         "atLeastOne",
			wantCount:        2,
			wantDynamicHosts: []string{"a"},
		},
		{
			name: "unmanaged only is skip",
			cluster: newCluster([]v1alpha1.StorageCephNode{osdHost("a", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{
				DataDevices: sel("/dev/sdb"), Unmanaged: true,
			})}, nil),
			wantMode:  "skip",
			wantCount: 0,
		},
		{
			name:      "no osd host is skip",
			cluster:   newCluster([]v1alpha1.StorageCephNode{osdHost("a", []string{"mon"}, nil, nil)}, nil),
			wantMode:  "skip",
			wantCount: 0,
		},
		{
			name: "fleet drivegroup counts per host",
			cluster: newCluster(
				[]v1alpha1.StorageCephNode{osdHost("a", []string{"osd"}, nil, nil), osdHost("b", []string{"osd"}, nil, nil)},
				[]v1alpha1.StorageCephOSDDrivegroup{{ServiceID: "f", OSD: v1alpha1.StorageCephNodeOSD{DataDevices: sel("/dev/sdb", "/dev/sdc")}}},
			),
			wantMode:  "exact",
			wantCount: 4,
		},
		{
			name: "dynamic hosts include node and fleet placements once",
			cluster: newCluster(
				[]v1alpha1.StorageCephNode{
					osdHost("a", []string{"osd"}, nil, &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}),
					osdHost("b", []string{"osd"}, nil, nil),
					osdHost("c", []string{"mon"}, nil, nil),
				},
				[]v1alpha1.StorageCephOSDDrivegroup{{
					ServiceID: "dynamic",
					OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{Rotational: new(bool)}},
				}},
			),
			wantMode:         "atLeastOne",
			wantCount:        2,
			wantDynamicHosts: []string{"a", "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, count, dynamicHosts := ceph.OSDReadinessExpectation(tc.cluster)
			if mode != tc.wantMode || count != tc.wantCount {
				t.Errorf("OSDReadinessExpectation = (%q, %d), want (%q, %d)", mode, count, tc.wantMode, tc.wantCount)
			}
			wantDynamicHosts := tc.wantDynamicHosts
			if wantDynamicHosts == nil {
				wantDynamicHosts = []string{}
			}
			if !reflect.DeepEqual(dynamicHosts, wantDynamicHosts) {
				t.Errorf("OSDReadinessExpectation dynamic hosts = %v, want %v", dynamicHosts, tc.wantDynamicHosts)
			}
		})
	}
}
