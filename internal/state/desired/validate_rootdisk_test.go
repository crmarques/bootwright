package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephNodeState(hints *v1alpha1.RootDeviceHints, devices []string) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	m.Spec.OS.Provided = v1alpha1.BoolPtr(false)
	m.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel"}
	m.Spec.OS.Install.RootDeviceHints = hints
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{m},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
					{Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Devices: devices},
				}},
			}},
		}},
	}
}

func TestValidateManagedOSCephNodeRootDisk(t *testing.T) {
	// OSD node, no root device -> clearpart --all would wipe OSD disks -> refuse.
	errs := validateManagedOSCephNodeRootDisk(cephNodeState(nil, []string{"/dev/sdb"}))
	if len(errs) != 1 || !strings.Contains(errs[0], "clearpart --all") {
		t.Fatalf("OSD node without root device must refuse, got %v", errs)
	}
	// OSD node WITH a root device -> scoped clearpart -> safe.
	ok := validateManagedOSCephNodeRootDisk(cephNodeState(&v1alpha1.RootDeviceHints{DeviceName: "/dev/sda"}, []string{"/dev/sdb"}))
	if len(ok) != 0 {
		t.Fatalf("OSD node with a root device must pass, got %v", ok)
	}
	// Node with no OSD devices -> nothing to preserve -> not gated.
	none := validateManagedOSCephNodeRootDisk(cephNodeState(nil, nil))
	if len(none) != 0 {
		t.Fatalf("node with no OSD devices must not be gated, got %v", none)
	}
}
