package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// cephNodeState builds a managed-OS ceph node. host is the OSD host under test;
// pass the selection shape (literal devices, host.osd drivegroup, or fleet
// drivegroup) to exercise each path the wipe guard must cover.
func cephNodeState(hints *v1alpha1.RootDeviceHints, host v1alpha1.StorageCephHost, drivegroups []v1alpha1.StorageCephOSDDrivegroup) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	m.Spec.OS.Provided = v1alpha1.BoolPtr(false)
	m.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel"}
	m.Spec.OS.Install.RootDeviceHints = hints
	host.Hostname = "ceph-0"
	host.MachineRef = v1alpha1.LocalObjectReference{Name: "ceph-0"}
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{m},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Hosts:          []v1alpha1.StorageCephHost{host},
					OSDDrivegroups: drivegroups,
				},
			}},
		}},
	}
}

func osdHost(devices []string, osd *v1alpha1.StorageCephHostOSD) v1alpha1.StorageCephHost {
	return v1alpha1.StorageCephHost{Roles: []string{v1alpha1.StorageCephRoleOSD}, Devices: devices, OSD: osd}
}

func TestValidateManagedOSCephNodeRootDisk(t *testing.T) {
	// OSD node (literal devices), no root device -> clearpart --all would wipe OSD
	// disks -> refuse.
	errs := validateManagedOSCephNodeRootDisk(cephNodeState(nil, osdHost([]string{"/dev/sdb"}, nil), nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "clearpart --all") {
		t.Fatalf("OSD node without root device must refuse, got %v", errs)
	}
	// OSD node WITH a root device -> scoped clearpart -> safe.
	ok := validateManagedOSCephNodeRootDisk(cephNodeState(&v1alpha1.RootDeviceHints{DeviceName: "/dev/sda"}, osdHost([]string{"/dev/sdb"}, nil), nil))
	if len(ok) != 0 {
		t.Fatalf("OSD node with a root device must pass, got %v", ok)
	}
	// OSD node selecting disks via the host.osd drivegroup arm (empty Devices) with
	// no root device -> still wiped -> refuse (the len(devices)==0 guard missed this).
	dg := validateManagedOSCephNodeRootDisk(cephNodeState(nil, osdHost(nil, &v1alpha1.StorageCephHostOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}), nil))
	if len(dg) != 1 || !strings.Contains(dg[0], "clearpart --all") {
		t.Fatalf("host.osd drivegroup node without root device must refuse, got %v", dg)
	}
	// Non-OSD node (no osd role) -> nothing to preserve -> not gated.
	none := validateManagedOSCephNodeRootDisk(cephNodeState(nil, v1alpha1.StorageCephHost{Roles: []string{v1alpha1.StorageCephRoleMON}}, nil))
	if len(none) != 0 {
		t.Fatalf("non-OSD node must not be gated, got %v", none)
	}
}
