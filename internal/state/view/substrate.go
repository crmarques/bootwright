package stateview

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type ClusterSubstrate struct {
	Provider string
	Host     string
}

func ContainerClusterSubstrate(state v1alpha1.State, cluster v1alpha1.ContainerCluster) ClusterSubstrate {
	providers := make(map[string]v1alpha1.InfraProvider, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	machines := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		machines[machine.Metadata.Name] = machine
	}
	seen := map[string]bool{}
	var types []string
	host := ""
	for _, node := range cluster.Spec.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok {
			continue
		}
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if !ok || provider.Spec.Type == "" {
			continue
		}
		if !seen[provider.Spec.Type] {
			seen[provider.Spec.Type] = true
			types = append(types, provider.Spec.Type)
		}
		if provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && host == "" &&
			provider.Spec.KubeVirt != nil && provider.Spec.KubeVirt.HostClusterRef != nil {
			host = provider.Spec.KubeVirt.HostClusterRef.Name
		}
	}
	if len(types) == 0 {
		return ClusterSubstrate{}
	}
	if seen[v1alpha1.ProvisionerKubeVirt] {
		return ClusterSubstrate{Provider: v1alpha1.ProvisionerKubeVirt, Host: host}
	}
	sort.Strings(types)
	return ClusterSubstrate{Provider: types[0]}
}
