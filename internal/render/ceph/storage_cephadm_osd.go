package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// cephadmOSDServices renders one per-host OSD service for every osd-role
// host. Validation guarantees each osd-role host authors devices or osd, so
// device consumption is always explicit — consuming all available devices is
// the authored osd: {dataDevices: {all: true}}, never an implicit default.
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
		// The OSD service id stays on the bare machine name: it becomes part of
		// daemon/systemd unit names, where a dotted FQDN is fragile. Placement
		// still targets node.Hostname so cephadm matches the registered host.
		doc := cephadmPlacementService("osd", "data-"+node.MachineRef.Name, []string{node.Hostname}, 0, spec)
		// unmanaged and the service-override keys are top-level service-spec keys
		// (siblings of spec/placement), not entries inside the drivegroup spec.
		applyCephOSDServiceFields(doc, node.OSD)
		docs = append(docs, doc)
	}
	// Fleet drivegroups: one OSD doc per entry, the authored serviceID, placement
	// resolved across the osd-role hosts (narrowable by sites/hosts). Validation
	// guarantees no host is owned by both a fleet and a per-host drivegroup.
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

// applyCephOSDServiceFields sets the top-level OSD service-spec keys (unmanaged
// and the common service overrides) on a rendered OSD doc.
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

// cephadmOSDSpec renders the drivegroup-shaped host OSD selection into the
// cephadm OSD service spec, field for field. unmanaged is intentionally absent:
// it is a top-level service-spec key set by the caller, not a spec entry.
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
		// The expanded path form: a list of {path, crush_device_class} mappings.
		// An entry with no class renders as a bare path so a mixed list (some
		// classed, some not) round-trips cleanly.
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
		// Upstream drivegroups spell rotational as 0/1.
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
