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
