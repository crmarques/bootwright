package inventory

import "github.com/crmarques/bootwright/api/v1alpha1"

func MachineInventoryHosts(state v1alpha1.State, machineName string) []string {
	var hosts []string
	seen := map[string]bool{}
	add := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machineName {
				add(AgentNodeHostName(cluster.Metadata.Name, machineName))
			}
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machineName {
				add(storageInventoryHostName(cluster, machineName))
			}
		}
	}
	return hosts
}
