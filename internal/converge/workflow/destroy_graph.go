package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func KubeVirtHostParentsByChild(state v1alpha1.State) map[string]map[string]bool {
	machines := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		machines[machine.Metadata.Name] = machine
	}
	providers := make(map[string]v1alpha1.InfraProvider, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	out := map[string]map[string]bool{}
	add := func(child, machineName string) {
		machine, ok := machines[machineName]
		if !ok {
			return
		}
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil {
			return
		}
		parent := provider.Spec.KubeVirt.HostClusterRef.Name
		if parent == "" || parent == child {
			return
		}
		if out[child] == nil {
			out[child] = map[string]bool{}
		}
		out[child][parent] = true
	}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			add(cluster.Metadata.Name, node.MachineRef.Name)
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			add(cluster.Metadata.Name, node.MachineRef.Name)
		}
	}
	return out
}

func KubeVirtHostParentNames(state v1alpha1.State) map[string][]string {
	parents := KubeVirtHostParentsByChild(state)
	out := make(map[string][]string, len(parents))
	for child, hosts := range parents {
		names := make([]string, 0, len(hosts))
		for name := range hosts {
			names = append(names, name)
		}
		sort.Strings(names)
		out[child] = names
	}
	return out
}

func destroyMachineInfraClusterGroup(state v1alpha1.State, cluster string) string {
	for _, candidate := range state.ContainerClusters {
		if candidate.Metadata.Name == cluster {
			return render.MachineInfraGroupName(cluster)
		}
	}
	return render.ManagedOSGroupName(cluster)
}

func destroyMachineInfraFanOutClusters(state v1alpha1.State) []string {
	groups := render.HostGroupMembers(state)
	var clusters []string
	for _, cluster := range state.ContainerClusters {
		clusters = append(clusters, cluster.Metadata.Name)
	}
	for _, cluster := range state.StorageClusters {
		if v1alpha1.StorageClusterManaged(cluster) {
			clusters = append(clusters, cluster.Metadata.Name)
		}
	}
	sort.Strings(clusters)
	out := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if len(groups[destroyMachineInfraClusterGroup(state, cluster)]) == 0 {
			continue
		}
		out = append(out, cluster)
	}
	if len(out) == 0 {
		return nil
	}
	for _, cluster := range clusters {
		if len(ClusterSubstrateMachineNames(state, cluster)) == 0 {
			continue
		}
		if len(groups[destroyMachineInfraClusterGroup(state, cluster)]) == 0 {
			return nil
		}
	}
	return out
}
