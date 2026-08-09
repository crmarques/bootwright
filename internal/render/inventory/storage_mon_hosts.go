package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func storageMonInventoryHosts(cluster v1alpha1.StorageCluster) []string {
	var hosts []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			hosts = append(hosts, storageInventoryHostName(cluster, node.MachineRef.Name))
		}
	}
	return hosts
}
