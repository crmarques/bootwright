package inventory

import "github.com/crmarques/bootwright/api/v1alpha1"

// MachineInventoryHosts returns the post-provision inventory host name(s) a
// Machine appears under: the agent-node host for each ContainerCluster that
// references it, and the storage-node host for each StorageCluster topology host
// that references it. It resolves a ProvisioningPlaybook target.machines entry to
// an ansible --limit token set; a machine referenced by no cluster returns none.
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
		for _, node := range cluster.Spec.Hosts {
			if node.MachineRef.Name == machineName {
				add(AgentNodeHostName(cluster.Metadata.Name, machineName))
			}
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if node.MachineRef.Name == machineName {
				add(storageInventoryHostName(cluster, machineName))
			}
		}
	}
	return hosts
}
