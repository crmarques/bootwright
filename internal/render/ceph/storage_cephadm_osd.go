package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func osdSelectionStaticPaths(sel *v1alpha1.StorageCephDeviceSelection) []string {
	if sel == nil {
		return nil
	}
	var paths []string
	paths = append(paths, sel.Paths...)
	for _, p := range sel.PathSpecs {
		if p.Path != "" {
			paths = append(paths, p.Path)
		}
	}
	return paths
}

func osdSelectionIsStatic(sel *v1alpha1.StorageCephDeviceSelection) bool {
	if sel == nil {
		return false
	}
	if sel.All || sel.Model != "" || sel.Vendor != "" || sel.Rotational != nil ||
		sel.Size != "" || sel.Limit != 0 {
		return false
	}
	return len(sel.Paths) > 0 || len(sel.PathSpecs) > 0
}

func osdHostSpecStaticPaths(osd *v1alpha1.StorageCephHostOSD) []string {
	if osd == nil {
		return nil
	}
	var paths []string
	paths = append(paths, osdSelectionStaticPaths(osd.DataDevices)...)
	paths = append(paths, osdSelectionStaticPaths(osd.DBDevices)...)
	paths = append(paths, osdSelectionStaticPaths(osd.WALDevices)...)
	return paths
}

func OSDGateDevicePaths(cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephHost) []string {
	seen := map[string]bool{}
	var out []string
	add := func(paths []string) {
		for _, p := range paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(node.Devices)
	add(osdHostSpecStaticPaths(node.OSD))
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		for _, host := range topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			if host == node.Hostname {
				add(osdHostSpecStaticPaths(&cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD))
				break
			}
		}
	}
	return out
}

func OSDReadinessExpectation(cluster v1alpha1.StorageCluster) (mode string, count int) {
	managed := false
	exact := true
	total := 0
	consider := func(osd *v1alpha1.StorageCephHostOSD, hosts int) {
		if osd == nil || osd.Unmanaged {
			return
		}
		managed = true
		if !osdSelectionIsStatic(osd.DataDevices) {
			exact = false
			return
		}
		per := osd.OSDsPerDevice
		if per < 1 {
			per = 1
		}
		total += hosts * len(osdSelectionStaticPaths(osd.DataDevices)) * per
	}
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		if len(node.Devices) > 0 {
			managed = true
			total += len(node.Devices)
			continue
		}
		consider(node.OSD, 1)
	}
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		hosts := len(topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD))
		consider(&cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD, hosts)
	}
	switch {
	case !managed:
		return "skip", 0
	case exact:
		return "exact", total
	default:
		return "atLeastOne", 0
	}
}

func cephadmOSDServices(cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) || (len(node.Devices) == 0 && node.OSD == nil) {
			continue
		}
		var spec map[string]any
		if node.OSD != nil {
			spec = cephadmOSDSpec(node.OSD)
		} else {
			spec = map[string]any{
				"data_devices": map[string]any{
					"paths": append([]string(nil), node.Devices...),
				},
			}
		}
		doc := cephadmPlacementService("osd", "data-"+node.MachineRef.Name, []string{node.Hostname}, 0, spec)
		applyCephOSDServiceFields(doc, node.OSD)
		docs = append(docs, doc)
	}
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		hosts := topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD)
		if len(hosts) == 0 {
			continue
		}
		doc := cephadmPlacementService("osd", dg.ServiceID, hosts, dg.Placement.CountPerHost, cephadmOSDSpec(&dg.OSD))
		applyCephOSDServiceFields(doc, &dg.OSD)
		docs = append(docs, doc)
	}
	return docs
}

func applyCephOSDServiceFields(doc map[string]any, osd *v1alpha1.StorageCephHostOSD) {
	if osd == nil {
		return
	}
	unmanaged := osd.Unmanaged
	if o := osd.ServiceOverrides; o != nil {
		applyCephServiceCommonFields(doc, unmanaged, o.ExtraContainerArgs, o.ExtraEntrypointArgs, o.Networks, o.CustomConfigs)
		return
	}
	if unmanaged {
		doc["unmanaged"] = true
	}
}

func cephadmOSDSpec(osd *v1alpha1.StorageCephHostOSD) map[string]any {
	spec := map[string]any{}
	if osd.DataDevices != nil {
		spec["data_devices"] = cephadmDeviceSelection(osd.DataDevices)
	}
	if osd.DBDevices != nil {
		spec["db_devices"] = cephadmDeviceSelection(osd.DBDevices)
	}
	if osd.WALDevices != nil {
		spec["wal_devices"] = cephadmDeviceSelection(osd.WALDevices)
	}
	if osd.FilterLogic != "" {
		spec["filter_logic"] = osd.FilterLogic
	}
	if osd.Encrypted {
		spec["encrypted"] = true
	}
	if osd.TPM2 {
		spec["tpm2"] = true
	}
	if osd.OSDsPerDevice > 0 {
		spec["osds_per_device"] = osd.OSDsPerDevice
	}
	if osd.CrushDeviceClass != "" {
		spec["crush_device_class"] = osd.CrushDeviceClass
	}
	if osd.BlockDBSize != "" {
		spec["block_db_size"] = osd.BlockDBSize
	}
	if osd.BlockWALSize != "" {
		spec["block_wal_size"] = osd.BlockWALSize
	}
	if osd.DBSlots > 0 {
		spec["db_slots"] = osd.DBSlots
	}
	if osd.WALSlots > 0 {
		spec["wal_slots"] = osd.WALSlots
	}
	if osd.DataAllocateFraction > 0 {
		spec["data_allocate_fraction"] = osd.DataAllocateFraction
	}
	return spec
}

func cephadmDeviceSelection(selection *v1alpha1.StorageCephDeviceSelection) map[string]any {
	out := map[string]any{}
	if len(selection.Paths) > 0 {
		out["paths"] = append([]string(nil), selection.Paths...)
	} else if len(selection.PathSpecs) > 0 {
		paths := make([]any, 0, len(selection.PathSpecs))
		for _, p := range selection.PathSpecs {
			if p.CrushDeviceClass == "" {
				paths = append(paths, p.Path)
				continue
			}
			paths = append(paths, map[string]any{
				"path":               p.Path,
				"crush_device_class": p.CrushDeviceClass,
			})
		}
		out["paths"] = paths
	}
	if selection.All {
		out["all"] = true
	}
	if selection.Model != "" {
		out["model"] = selection.Model
	}
	if selection.Vendor != "" {
		out["vendor"] = selection.Vendor
	}
	if selection.Rotational != nil {
		rotational := 0
		if *selection.Rotational {
			rotational = 1
		}
		out["rotational"] = rotational
	}
	if selection.Size != "" {
		out["size"] = selection.Size
	}
	if selection.Limit > 0 {
		out["limit"] = selection.Limit
	}
	return out
}
