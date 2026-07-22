package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateOSDDevicesExcludeRootDisk(state v1alpha1.State) []string {
	var errs []string
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Nodes {
			if host.MachineRef.Name == "" {
				continue
			}
			machine, ok := stateview.Machine(state, host.MachineRef.Name)
			if !ok || machine.Spec.OS.Install.RootDeviceHints == nil {
				continue
			}
			root := strings.TrimSpace(machine.Spec.OS.Install.RootDeviceHints.DeviceName)
			if root == "" {
				continue
			}
			for _, dev := range topology.OSDHostAllStaticDevices(sc, host) {
				if strings.TrimSpace(dev) == root {
					errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) lists its OS root disk %q as an OSD device; creating the OSD would wipe the installed operating system. Remove %q from the OSD device selection, or point rootDeviceHints at a different disk", sc.Metadata.Name, host.Name, host.MachineRef.Name, root, root))
					break
				}
			}
		}
	}
	return errs
}

func validateManagedOSCephNodeRootDisk(state v1alpha1.State) []string {
	var errs []string
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Nodes {
			if !topology.NodeHasRole(host, v1alpha1.StorageCephRoleOSD) || host.MachineRef.Name == "" {
				continue
			}
			machine, ok := stateview.Machine(state, host.MachineRef.Name)
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			hints := machine.Spec.OS.Install.RootDeviceHints
			if hints != nil && strings.TrimSpace(hints.DeviceName) != "" {
				continue
			}
			errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) carries the %q role (OSD data disks) but its managed-OS install has no spec.os.install.rootDeviceHints.deviceName; the install would clearpart --all and WIPE those OSD data disks. Set the root device so the install targets only the OS disk", sc.Metadata.Name, host.Name, host.MachineRef.Name, v1alpha1.StorageCephRoleOSD))
		}
	}
	return errs
}
