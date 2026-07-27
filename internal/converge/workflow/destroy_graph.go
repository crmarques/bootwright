package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

type destroyGraphFacts struct {
	containerClusters  []string
	storageClusters    []string
	machineInfraFan    []string
	machineInfraFanSet map[string]bool
	parents            map[string]map[string]bool
	guestsByHost       map[string][]string
	placement          map[string]bool
	consumersByStorage map[string][]string
}

func destroyGraphFactsFor(state v1alpha1.State) destroyGraphFacts {
	facts := destroyGraphFacts{
		parents:            KubeVirtHostParentsByChild(state),
		consumersByStorage: stategraph.StorageConsumersByCluster(state),
		machineInfraFanSet: map[string]bool{},
	}
	for _, cluster := range state.ContainerClusters {
		facts.containerClusters = append(facts.containerClusters, cluster.Metadata.Name)
	}
	for _, cluster := range state.StorageClusters {
		facts.storageClusters = append(facts.storageClusters, cluster.Metadata.Name)
	}
	sort.Strings(facts.containerClusters)
	sort.Strings(facts.storageClusters)
	facts.machineInfraFan = destroyMachineInfraFanOutClusters(state)
	for _, name := range facts.machineInfraFan {
		facts.machineInfraFanSet[name] = true
	}
	facts.guestsByHost = destroyGuestsByHost(facts.parents)
	facts.placement = destroyPlacementClosure(state, facts.parents)
	return facts
}

func destroyGuestsByHost(parents map[string]map[string]bool) map[string][]string {
	out := map[string][]string{}
	for child, hosts := range parents {
		for host := range hosts {
			out[host] = append(out[host], child)
		}
	}
	for host := range out {
		sort.Strings(out[host])
	}
	return out
}

func destroyClusterOwnerByMachine(state v1alpha1.State) map[string]string {
	out := map[string]string{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name != "" {
				out[node.MachineRef.Name] = cluster.Metadata.Name
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name != "" {
				out[node.MachineRef.Name] = cluster.Metadata.Name
			}
		}
	}
	return out
}

func destroyPlacementClosure(state v1alpha1.State, parents map[string]map[string]bool) map[string]bool {
	owner := destroyClusterOwnerByMachine(state)
	out := map[string]bool{}
	for _, host := range render.HostGroupMembers(state)[render.GroupInfraComponentHosts] {
		if cluster := owner[host]; cluster != "" {
			out[cluster] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for child := range out {
			for parent := range parents[child] {
				if !out[parent] {
					out[parent] = true
					changed = true
				}
			}
		}
	}
	return out
}

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
	if len(out) < 2 {
		return nil
	}
	return out
}
