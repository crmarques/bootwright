package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// validateOSDDevicesExcludeRootDisk fails closed when a Ceph node lists its own OS
// root disk (rootDeviceHints.deviceName) among its OSD data/DB/WAL devices. cephadm
// would ceph-volume that disk into an OSD, wiping the installed operating system the
// node boots from. This complements the root-disk requirement: the OS disk must be
// resolved AND must not double as an OSD. Only explicit device PATHS are checked (a
// filter/`all: true` selection is left to cephadm's own "available device" filter,
// which excludes the mounted root); an empty rootDeviceHints is covered by the
// root-disk requirement above.
func validateOSDDevicesExcludeRootDisk(state v1alpha1.State) []string {
	var errs []string
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Hosts {
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
			for _, dev := range osdDeclaredDevicePaths(host) {
				if strings.TrimSpace(dev) == root {
					errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) lists its OS root disk %q as an OSD device; creating the OSD would wipe the installed operating system. Remove %q from the OSD device selection, or point rootDeviceHints at a different disk", sc.Metadata.Name, host.Hostname, host.MachineRef.Name, root, root))
					break
				}
			}
		}
	}
	return errs
}

// osdDeclaredDevicePaths collects every explicit device PATH a ceph host declares as
// an OSD device — the lean `devices` shorthand and the drivegroup-shaped
// osd.dataDevices/dbDevices/walDevices path lists.
func osdDeclaredDevicePaths(host v1alpha1.StorageCephHost) []string {
	paths := append([]string{}, host.Devices...)
	if host.OSD != nil {
		for _, sel := range []*v1alpha1.StorageCephDeviceSelection{host.OSD.DataDevices, host.OSD.DBDevices, host.OSD.WALDevices} {
			if sel != nil {
				paths = append(paths, sel.Paths...)
			}
		}
	}
	return paths
}

// validateManagedOSCephNodeRootDisk fails closed when a Ceph OSD node installs its
// OS via bootwright (managed OS) but declares no root device. The Anaconda kickstart
// scopes its disk wipe to storage.rootDisk when one is resolved, but falls back to an
// unconditional `clearpart --all` when it is empty — which on an OSD node ALSO wipes
// the data disks the node is supposed to host, destroying the cluster's OSDs the next
// time the node is (re)installed (e.g. removing rootDeviceHints then reconciling with
// --override). rootDisk resolves only from spec.os.install.rootDeviceHints.deviceName,
// so requiring it here guarantees the install targets the OS disk and leaves the OSD
// devices intact. A node with no declared OSD devices has nothing to preserve and is
// not gated.
func validateManagedOSCephNodeRootDisk(state v1alpha1.State) []string {
	var errs []string
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Hosts {
			// Any osd-role host carries OSD data disks, whether selected via the
			// literal `devices` shorthand, the `osd` drivegroup arm, or a fleet
			// osdDrivegroup — all of which require/target the osd role (the lean
			// `len(devices)>0` check missed the drivegroup and fleet shapes, which
			// leave Devices empty, so their OSD disks were silently wiped).
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
			errs = append(errs, fmt.Sprintf("StorageCluster/%s ceph node %q (Machine/%s) carries the %q role (OSD data disks) but its managed-OS install has no spec.os.install.rootDeviceHints.deviceName; the install would clearpart --all and WIPE those OSD data disks. Set the root device so the install targets only the OS disk", sc.Metadata.Name, host.Hostname, host.MachineRef.Name, v1alpha1.StorageCephRoleOSD))
		}
	}
	return errs
}
