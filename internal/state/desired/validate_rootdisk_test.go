package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephNodeState(hints *v1alpha1.RootDeviceHints, host v1alpha1.StorageCephNode, drivegroups []v1alpha1.StorageCephOSDDrivegroup) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	m.Spec.OS.Provided = v1alpha1.BoolPtr(false)
	m.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel"}
	m.Spec.OS.Install.RootDeviceHints = hints
	host.Name = "ceph-0"
	host.MachineRef = v1alpha1.LocalObjectReference{Name: "ceph-0"}
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{m},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes:          []v1alpha1.StorageCephNode{host},
					OSDDrivegroups: drivegroups,
				},
			}},
		}},
	}
}

func osdHost(devices []string, osd *v1alpha1.StorageCephNodeOSD) v1alpha1.StorageCephNode {
	return v1alpha1.StorageCephNode{Roles: []string{v1alpha1.StorageCephRoleOSD}, Devices: devices, OSD: osd}
}

func TestValidateManagedOSCephNodeRootDisk(t *testing.T) {
	errs := validateManagedOSCephNodeRootDisk(cephNodeState(nil, osdHost([]string{"/dev/sdb"}, nil), nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "clearpart --all") {
		t.Fatalf("OSD node without root device must refuse, got %v", errs)
	}
	ok := validateManagedOSCephNodeRootDisk(cephNodeState(&v1alpha1.RootDeviceHints{DeviceName: "/dev/sda"}, osdHost([]string{"/dev/sdb"}, nil), nil))
	if len(ok) != 0 {
		t.Fatalf("OSD node with a root device must pass, got %v", ok)
	}
	dg := validateManagedOSCephNodeRootDisk(cephNodeState(nil, osdHost(nil, &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}), nil))
	if len(dg) != 1 || !strings.Contains(dg[0], "clearpart --all") {
		t.Fatalf("host.osd drivegroup node without root device must refuse, got %v", dg)
	}
	none := validateManagedOSCephNodeRootDisk(cephNodeState(nil, v1alpha1.StorageCephNode{Roles: []string{v1alpha1.StorageCephRoleMON}}, nil))
	if len(none) != 0 {
		t.Fatalf("non-OSD node must not be gated, got %v", none)
	}
}
