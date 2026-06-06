package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/ownership"
)

// HostGroupCounts returns the number of hosts in each inventory child
// group for the given state. Used to detect an ansible-playbook
// invocation that would target only empty groups (which fails with
// "no hosts to target") and skip it instead. Controller and OCP-install
// groups contain localhost when clusters are loaded.
func HostGroupCounts(state v1alpha1.State) map[string]int {
	return HostGroupCountsWithOwnershipRecords(state, nil)
}

func HostGroupCountsWithOwnershipRecords(state v1alpha1.State, records []ownership.ResourceRecord) map[string]int {
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	storageHostGroups := storageClusterHostSets(state)
	machineTaskHostSet, machineTaskGroups := machineTaskHostSets(state)
	recorded := ownershipInventory(records)
	out := map[string]int{
		GroupInfraHosts:          len(mergeHostSets(infraReferencedHosts(state), recorded.InfraHosts)),
		GroupProviderHosts:       len(mergeHostSets(providerReferencedHosts(state), recorded.ProviderHosts)),
		GroupInfraComponentHosts: len(mergeHostSets(infraComponentReferencedHosts(state), recorded.InfraComponentHosts)),
		GroupBootHosts:           len(bootReferencedHosts(state)),
		GroupControllerHosts:     len(ocpReferencedHosts(state)),
		GroupOCPHosts:            len(ocpReferencedHosts(state)),
		GroupAgentNodeHosts:      len(agentNodeHostSet),
		GroupMachineTaskHosts:    len(machineTaskHostSet),
		GroupStorageHosts:        len(mergeHostSets(storageReferencedHosts(state), recorded.StorageHosts)),
	}
	for group, set := range agentNodeGroups {
		out[group] = len(set)
	}
	for group, set := range machineTaskGroups {
		out[group] = len(set)
	}
	for group, set := range storageHostGroups {
		out[group] = len(set)
	}
	return out
}

func HostGroupMembers(state v1alpha1.State) map[string][]string {
	return HostGroupMembersWithOwnershipRecords(state, nil)
}

func HostGroupMembersWithOwnershipRecords(state v1alpha1.State, records []ownership.ResourceRecord) map[string][]string {
	ocpHosts := sortedHostSet(ocpReferencedHosts(state))
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	storageHostGroups := storageClusterHostSets(state)
	machineTaskHostSet, machineTaskGroups := machineTaskHostSets(state)
	recorded := ownershipInventory(records)
	out := map[string][]string{
		GroupInfraHosts:          sortedHostSet(mergeHostSets(infraReferencedHosts(state), recorded.InfraHosts)),
		GroupProviderHosts:       sortedHostSet(mergeHostSets(providerReferencedHosts(state), recorded.ProviderHosts)),
		GroupInfraComponentHosts: sortedHostSet(mergeHostSets(infraComponentReferencedHosts(state), recorded.InfraComponentHosts)),
		GroupBootHosts:           sortedHostSet(bootReferencedHosts(state)),
		GroupControllerHosts:     ocpHosts,
		GroupOCPHosts:            ocpHosts,
		GroupAgentNodeHosts:      sortedHostSet(agentNodeHostSet),
		GroupMachineTaskHosts:    sortedHostSet(machineTaskHostSet),
		GroupStorageHosts:        sortedHostSet(mergeHostSets(storageReferencedHosts(state), recorded.StorageHosts)),
	}
	for group, set := range agentNodeGroups {
		out[group] = sortedHostSet(set)
	}
	for group, set := range machineTaskGroups {
		out[group] = sortedHostSet(set)
	}
	for group, set := range storageHostGroups {
		out[group] = sortedHostSet(set)
	}
	return out
}
