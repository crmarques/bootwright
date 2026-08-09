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
			if _, selectable := v1alpha1.AnacondaRootDiskSelector(hints); selectable {
				errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) carries the %q role (OSD data disks) and names its root disk by a hint other than spec.os.install.rootDeviceHints.deviceName. The install itself is scoped correctly, but an OSD node must name the root disk the same way its OSD devices are named, so that declaring the OS disk as an OSD device can be refused before ceph-volume wipes it. Set deviceName to the same path form as the OSD device selection", sc.Metadata.Name, host.Name, host.MachineRef.Name, v1alpha1.StorageCephRoleOSD))
				continue
			}
			errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) carries the %q role (OSD data disks) but its managed-OS install has no spec.os.install.rootDeviceHints.deviceName; the install would clearpart --all and WIPE those OSD data disks. Set the root device so the install targets only the OS disk", sc.Metadata.Name, host.Name, host.MachineRef.Name, v1alpha1.StorageCephRoleOSD))
		}
	}
	return errs
}

func validateStorageCephRootFilesystemProfiles(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		binding, ok := topology.ResolveNodeMachineProfile(state, node)
		if !ok || binding.EffectiveDiskGiB >= topology.RootFilesystemFloorGiB {
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"StorageCluster/%s ceph node %q (Machine/%s) resolves to InfraProvider/%s type %q machine profile %q with diskGiB %d, below the absolute Ceph root-filesystem floor of %d GiB; the live preflight requires %d GiB free, so a raw disk smaller than that can never pass. Raise InfraProvider/%s spec.%s.machineProfiles profile %q diskGiB to at least %d before apply",
			cluster.Metadata.Name,
			node.Name,
			binding.Machine.Metadata.Name,
			binding.Provider.Metadata.Name,
			binding.Provider.Spec.Type,
			binding.Profile.Name,
			binding.EffectiveDiskGiB,
			topology.RootFilesystemFloorGiB,
			topology.RootFilesystemFloorGiB,
			binding.Provider.Metadata.Name,
			binding.Provider.Spec.Type,
			binding.Profile.Name,
			topology.RootFilesystemFloorGiB,
		))
	}
	return errs
}
