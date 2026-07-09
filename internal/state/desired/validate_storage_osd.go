package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStorageCephHostOSD(owner string, node v1alpha1.StorageCephHost, fleetCovered bool) []string {
	var errs []string
	if len(node.Devices) > 0 && node.OSD != nil {
		errs = append(errs, fmt.Sprintf("%s sets both devices and osd; devices is the shorthand for osd.dataDevices.paths — use one", owner))
	}
	hasOSDRole := topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD)
	if len(node.Devices) > 0 && !hasOSDRole {
		errs = append(errs, fmt.Sprintf("%s.devices requires the %q role", owner, v1alpha1.StorageCephRoleOSD))
	}
	errs = append(errs, validateStorageDevicePaths(owner+".devices", node.Devices)...)
	if fleetCovered && (len(node.Devices) > 0 || node.OSD != nil) {
		errs = append(errs, fmt.Sprintf("%s authors a per-host osd/devices but is also covered by a fleet osdDrivegroup; a host is owned by one OSD spec", owner))
	}
	if hasOSDRole && len(node.Devices) == 0 && node.OSD == nil && !fleetCovered {
		errs = append(errs, fmt.Sprintf("%s carries the %q role but selects no devices; author devices, osd.dataDevices, or cover it with a fleet osdDrivegroup", owner, v1alpha1.StorageCephRoleOSD))
	}
	if node.OSD == nil {
		return errs
	}
	if !hasOSDRole {
		errs = append(errs, fmt.Sprintf("%s.osd requires the %q role", owner, v1alpha1.StorageCephRoleOSD))
	}
	errs = append(errs, validateStorageCephOSDSpec(owner+".osd", node.OSD)...)
	return errs
}

func storageFleetCoveredHosts(cluster v1alpha1.StorageCluster) map[string]bool {
	covered := map[string]bool{}
	for _, dg := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		for _, host := range topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			covered[host] = true
		}
	}
	return covered
}

func validateStorageCephOSDDrivegroups(prefix string, cluster v1alpha1.StorageCluster) []string {
	var errs []string
	seenID := map[string]bool{}
	claimed := map[string]string{}
	for i, dg := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if dg.ServiceID == "" {
			errs = append(errs, owner+".serviceID is required")
		} else if seenID[dg.ServiceID] {
			errs = append(errs, fmt.Sprintf("%s.serviceID %q is duplicated", owner, dg.ServiceID))
		}
		seenID[dg.ServiceID] = true
		errs = append(errs, validateStoragePlacementHosts(owner+".placement", dg.Placement, cluster, true, v1alpha1.StorageCephRoleOSD)...)
		errs = append(errs, validateStorageCephOSDSpec(owner+".osd", &cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD)...)
		for _, host := range topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			if other, dup := claimed[host]; dup {
				errs = append(errs, fmt.Sprintf("%s host %q is also claimed by fleet osdDrivegroup %q; a host is owned by one OSD spec", owner, host, other))
			}
			claimed[host] = dg.ServiceID
		}
	}
	return errs
}

func validateStorageCephOSDSpec(owner string, osd *v1alpha1.StorageCephHostOSD) []string {
	var errs []string
	if osd.DataDevices == nil {
		errs = append(errs, owner+".dataDevices is required")
	}
	for _, selection := range []struct {
		field string
		value *v1alpha1.StorageCephDeviceSelection
	}{
		{"dataDevices", osd.DataDevices},
		{"dbDevices", osd.DBDevices},
		{"walDevices", osd.WALDevices},
	} {
		if selection.value == nil {
			continue
		}
		errs = append(errs, validateStorageCephDeviceSelection(owner+"."+selection.field, selection.value)...)
	}
	switch osd.FilterLogic {
	case "", "AND", "OR":
	default:
		errs = append(errs, fmt.Sprintf("%s.filterLogic %q must be AND or OR", owner, osd.FilterLogic))
	}
	if osd.TPM2 && !osd.Encrypted {
		errs = append(errs, owner+".tpm2 requires encrypted: true")
	}
	if osd.OSDsPerDevice < 0 {
		errs = append(errs, owner+".osdsPerDevice must be non-negative")
	}
	if osd.DBSlots < 0 {
		errs = append(errs, owner+".dbSlots must be non-negative")
	}
	if osd.WALSlots < 0 {
		errs = append(errs, owner+".walSlots must be non-negative")
	}
	if frac := osd.DataAllocateFraction; frac < 0 || frac > 1 {
		errs = append(errs, fmt.Sprintf("%s.dataAllocateFraction %v must be in (0, 1]", owner, frac))
	}
	if o := osd.ServiceOverrides; o != nil {
		errs = append(errs, validateStorageServiceOverrides(owner+".serviceOverrides", o)...)
	}
	return errs
}

func validateStorageServiceOverrides(owner string, o *v1alpha1.StorageCephServiceOverrides) []string {
	var errs []string
	for i, cidr := range o.Networks {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks[%d]", owner, i), cidr)...)
	}
	for i, cc := range o.CustomConfigs {
		entry := fmt.Sprintf("%s.customConfigs[%d]", owner, i)
		if cc.MountPath == "" {
			errs = append(errs, entry+".mountPath is required")
		} else if !strings.HasPrefix(cc.MountPath, "/") {
			errs = append(errs, fmt.Sprintf("%s.mountPath %q must be absolute", entry, cc.MountPath))
		}
		if cc.Content == "" {
			errs = append(errs, entry+".content must not be empty")
		}
	}
	return errs
}

func validateStorageCephDeviceSelection(owner string, selection *v1alpha1.StorageCephDeviceSelection) []string {
	var errs []string
	pathForms := 0
	if len(selection.Paths) > 0 {
		pathForms++
	}
	if len(selection.PathSpecs) > 0 {
		pathForms++
	}
	if pathForms > 0 && selection.All {
		errs = append(errs, owner+" must not set both an explicit path list and all")
	}
	if pathForms > 1 {
		errs = append(errs, owner+" must set only one of paths or pathSpecs")
	}
	errs = append(errs, validateStorageDevicePaths(owner+".paths", selection.Paths)...)
	pathSpecPaths := make([]string, 0, len(selection.PathSpecs))
	for i, p := range selection.PathSpecs {
		if p.Path == "" {
			errs = append(errs, fmt.Sprintf("%s.pathSpecs[%d].path is required", owner, i))
			continue
		}
		pathSpecPaths = append(pathSpecPaths, p.Path)
	}
	errs = append(errs, validateStorageDevicePaths(owner+".pathSpecs", pathSpecPaths)...)
	selectsSomething := pathForms > 0 || selection.All || selection.Model != "" ||
		selection.Vendor != "" || selection.Rotational != nil || selection.Size != "" || selection.Limit != 0
	if !selectsSomething {
		errs = append(errs, owner+" must select devices (paths, pathSpecs, all, model, vendor, rotational, size, or limit)")
	}
	errs = append(errs, validateStorageCephDeviceSize(owner+".size", selection.Size)...)
	if selection.Limit < 0 {
		errs = append(errs, owner+".limit must be non-negative")
	}
	return errs
}

func validateStorageDevicePaths(owner string, paths []string) []string {
	var errs []string
	seen := map[string]bool{}
	for i, path := range paths {
		trimmed := strings.TrimSpace(path)
		switch {
		case trimmed == "":
			errs = append(errs, fmt.Sprintf("%s[%d] must not be empty", owner, i))
			continue
		case !strings.HasPrefix(trimmed, "/dev/"):
			errs = append(errs, fmt.Sprintf("%s[%d] %q must be an absolute /dev path (e.g. /dev/sdb or /dev/disk/by-id/...)", owner, i, path))
			continue
		}
		if seen[trimmed] {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is listed more than once", owner, i, path))
		}
		seen[trimmed] = true
	}
	return errs
}

func validateStorageCephDeviceSize(owner, size string) []string {
	if size == "" {
		return nil
	}
	if strings.Count(size, ":") > 1 {
		return []string{fmt.Sprintf("%s %q is not a valid size range (use low:high, :high, or low:, e.g. 10G:40G)", owner, size)}
	}
	if strings.Contains(size, "-") {
		return []string{fmt.Sprintf("%s %q uses '-' as a range separator; cephadm sizes use ':' (e.g. 10G:40G)", owner, size)}
	}
	return nil
}
