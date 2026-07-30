package converge

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func reclaimTestState() v1alpha1.State {
	host := func(devs ...string) v1alpha1.StorageCephNode {
		return v1alpha1.StorageCephNode{Devices: devs}
	}
	cluster := func(name string, hosts ...v1alpha1.StorageCephNode) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{Nodes: hosts},
				},
			},
		}
	}
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			cluster("ceph1", host("/dev/sdb"), host("/dev/sdb")),
			cluster("ceph2", host("/dev/nvme0n1")),
		},
	}
}

func TestUnmatchedReclaimDevicesReportsUnmatched(t *testing.T) {
	state := reclaimTestState()
	unmatched, declared := UnmatchedReclaimDevices(state, []string{"ceph1"}, "/dev/disk/by-id/wwn-0x5000, /dev/sdb")
	if !reflect.DeepEqual(unmatched, []string{"/dev/disk/by-id/wwn-0x5000"}) {
		t.Fatalf("unmatched = %v, want [/dev/disk/by-id/wwn-0x5000]", unmatched)
	}
	if !reflect.DeepEqual(declared, []string{"/dev/sdb"}) {
		t.Fatalf("declared = %v, want [/dev/sdb]", declared)
	}
}

func TestUnmatchedReclaimDevicesAllMatch(t *testing.T) {
	state := reclaimTestState()
	unmatched, _ := UnmatchedReclaimDevices(state, []string{"ceph1", "ceph2"}, "/dev/sdb,/dev/nvme0n1")
	if unmatched != nil {
		t.Fatalf("unmatched = %v, want nil when every entry matches a declared device", unmatched)
	}
}

func TestDeclaredOwnedOSDDevicesCoversEveryStaticSelectionShape(t *testing.T) {
	rotational := false
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{
							{Name: "node-01", Devices: []string{"/dev/sdb"}},
							{Name: "node-02", Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{
								DataDevices: &v1alpha1.StorageCephDeviceSelection{
									Paths:     []string{"/dev/nvme0n1"},
									PathSpecs: []v1alpha1.StorageCephDevicePath{{Path: "/dev/nvme1n1"}},
								},
								DBDevices: &v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/nvme2n1"}},
							}},
							{Name: "node-03", Roles: []string{v1alpha1.StorageCephRoleOSD}},
							{Name: "node-04", Roles: []string{v1alpha1.StorageCephRoleOSD}, OSD: &v1alpha1.StorageCephNodeOSD{
								DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
							}},
						},
						OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
							ServiceID: "spinners",
							OSD: v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{
								Paths:      []string{"/dev/sdc"},
								Rotational: &rotational,
							}},
						}},
					},
				},
			},
		}},
	}
	want := []string{"/dev/nvme0n1", "/dev/nvme1n1", "/dev/nvme2n1", "/dev/sdb", "/dev/sdc"}
	if got := DeclaredOwnedOSDDevices(state, []string{"ceph1"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("DeclaredOwnedOSDDevices = %v, want %v; the CLI match set must equal the paths the ansible OSD gate enforces (host.devices + osd data/db/wal paths + pathSpecs + covering drivegroups), or install.yml tells the operator to run --reclaim-devices on a path the CLI then rejects", got, want)
	}
}

func TestDeclaredOwnedOSDDevicesOmitsFilterOnlySelections(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
						Name:  "node-01",
						Roles: []string{v1alpha1.StorageCephRoleOSD},
						OSD:   &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}},
					}}},
				},
			},
		}},
	}
	if got := DeclaredOwnedOSDDevices(state, []string{"ceph1"}); len(got) != 0 {
		t.Fatalf("DeclaredOwnedOSDDevices = %v, want empty: an all:true selection names no static path, so --reclaim-devices cannot match one and the operator must be steered to --mode rebuild --authorize data-loss instead", got)
	}
}

func TestUnmatchedReclaimDevicesIgnoresUnownedClusterDevices(t *testing.T) {
	state := reclaimTestState()
	unmatched, declared := UnmatchedReclaimDevices(state, []string{"ceph1"}, "/dev/nvme0n1")
	if !reflect.DeepEqual(unmatched, []string{"/dev/nvme0n1"}) {
		t.Fatalf("unmatched = %v, want [/dev/nvme0n1] since ceph2 is not owned here", unmatched)
	}
	if !reflect.DeepEqual(declared, []string{"/dev/sdb"}) {
		t.Fatalf("declared = %v, want only ceph1 devices", declared)
	}
}

func TestUnmatchedReclaimDevicesEmptyInput(t *testing.T) {
	state := reclaimTestState()
	if unmatched, declared := UnmatchedReclaimDevices(state, []string{"ceph1"}, "  ,  "); unmatched != nil || declared != nil {
		t.Fatalf("empty device input must yield no unmatched/declared, got %v / %v", unmatched, declared)
	}
}
