package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func osdNodeState(root string, hostDevices []string, osd *v1alpha1.StorageCephHostOSD) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	m.Spec.OS.Install.RootDeviceHints = &v1alpha1.RootDeviceHints{DeviceName: root}
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{m},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
					{Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Devices: hostDevices, OSD: osd},
				}},
			}},
		}},
	}
}

func TestValidateOSDDevicesExcludeRootDisk(t *testing.T) {
	errs := validateOSDDevicesExcludeRootDisk(osdNodeState("/dev/sda", []string{"/dev/sda", "/dev/sdb"}, nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "wipe the installed operating system") {
		t.Fatalf("root disk as OSD device must refuse, got %v", errs)
	}
	errs = validateOSDDevicesExcludeRootDisk(osdNodeState("/dev/sda", nil, &v1alpha1.StorageCephHostOSD{
		DataDevices: &v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/sda"}},
	}))
	if len(errs) != 1 {
		t.Fatalf("root disk in osd.dataDevices.paths must refuse, got %v", errs)
	}
	if e := validateOSDDevicesExcludeRootDisk(osdNodeState("/dev/sda", []string{"/dev/sdb", "/dev/sdc"}, nil)); len(e) != 0 {
		t.Fatalf("OSD devices distinct from root disk must pass, got %v", e)
	}
}
