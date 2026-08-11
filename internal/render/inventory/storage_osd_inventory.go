package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	cephrender "github.com/crmarques/bootwright/internal/render/ceph"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func storageHostsVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		host := map[string]any{
			"hostname":      node.MachineRef.Name,
			"cephHostname":  node.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.MachineRef.Name),
			"address":       topology.NodeAddress(state, cluster, node.MachineRef.Name),
			"devices":       cephrender.OSDGateDevicePaths(cluster, node),
			"rootFSGiB":     topology.NodeRootFilesystemGiB(cluster, node),
		}
		if cluster.Spec.Ceph.Security.FIPS.Enabled {
			host["fipsRequired"] = true
		}
		if node.Site != "" {
			host["site"] = node.Site
		}
		if topology.OSDHostUsesAllDevices(cluster, node) {
			host["osdReclaimAll"] = true
		}
		if filters := storageOSDDeviceFiltersVars(cluster, node); len(filters) > 0 {
			host["osdDeviceFilters"] = filters
		}
		out = append(out, host)
	}
	return out
}

func storageOSDDeviceFiltersVars(cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephNode) []any {
	selections := topology.DynamicOSDDeviceSelections(cluster, node)
	var out []any
	for _, role := range []string{"data", "db", "wal"} {
		selection := selections[role]
		if selection == nil {
			continue
		}
		filter := map[string]any{
			"role":        role,
			"filterLogic": topology.OSDFilterLogic(cluster, node),
		}
		if selection.All {
			filter["all"] = true
		}
		if selection.Model != "" {
			filter["model"] = selection.Model
		}
		if selection.Vendor != "" {
			filter["vendor"] = selection.Vendor
		}
		if selection.Rotational != nil {
			filter["rotational"] = *selection.Rotational
		}
		if selection.Size != "" {
			filter["size"] = selection.Size
		}
		if selection.Limit > 0 {
			filter["limit"] = selection.Limit
		}
		out = append(out, filter)
	}
	return out
}
