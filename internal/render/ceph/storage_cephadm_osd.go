package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// osdSelectionStaticPaths returns the explicit block-device paths a device
// selection names (its paths and pathSpecs[].path). It is nil when the selection
// carries no explicit paths (a filter/all selection, resolved on-host by
// ceph-volume, not statically).
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

// osdSelectionIsStatic reports whether a selection consumes only explicitly
// named devices — no all/model/vendor/rotational/size/limit filter — so its OSD
// device set (and count) is known at render time.
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

// osdHostSpecStaticPaths returns every explicit data/db/wal device path a
// drivegroup-shaped OSD spec names — the block devices Bootwright hands to
// ceph-volume, which the device-safety gates and ownership marker must cover.
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

// OSDGateDevicePaths returns the explicit block-device paths on this node that
// the apply/preflight device-empty gates, the OSD ownership marker, and the
// destroy wipe must all cover: the devices shorthand, the per-host drivegroup's
// explicit data/db/wal paths, and any covering fleet osdDrivegroup's explicit
// paths — deduplicated, shorthand first. Filter/all selections are not
// statically enumerable and are omitted (only ceph-volume's own refusal backs
// those); a drivegroup host authored purely by filter still gets no gate, which
// the storage docs call out.
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

// OSDReadinessExpectation summarizes what a post-apply readiness poll can assert
// about OSD creation for a cluster, so a fire-and-forget `ceph orch apply` can no
// longer report success against zero OSDs:
//
//	"exact"      — every managed OSD selection names explicit devices, so count is
//	               the exact number of OSD daemons to expect up and in.
//	"atLeastOne" — at least one managed OSD selection is filter/all-based (count is
//	               resolved on-host), so only "> 0 OSDs" is assertable.
//	"skip"       — no managed OSD service actively creates OSDs (no osd host, or
//	               every OSD service is unmanaged); the poll makes no assertion.
//
// osdsPerDevice multiplies the exact count; an unmanaged service contributes no
// new OSDs and is excluded.
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
