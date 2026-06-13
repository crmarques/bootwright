package stateview

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ClusterSubstrate describes how a ContainerCluster's node machines are
// produced: the provisioner that backs them and, for KubeVirt-hosted guests,
// the host cluster they run on. It is the data the CLI surfaces need to tell a
// bare-metal cluster apart from a KubeVirt-hosted one and to show the
// parent->child hosting link.
type ClusterSubstrate struct {
	// Provider is the provisioner backing the cluster's nodes
	// (v1alpha1.ProvisionerBareMetal / ProvisionerKubeVirt / ProvisionerLibvirt
	// / ProvisionerVSphere), or "" when it cannot be resolved.
	Provider string
	// Host is the KubeVirt host cluster name (the provider's HostClusterRef),
	// set only when Provider is KubeVirt and the ref is present.
	Host string
}

// ContainerClusterSubstrate resolves a cluster's backing provisioner by walking
// its node machines to their InfraProvider. A cluster whose nodes mix
// provisioners reports KubeVirt when any node is KubeVirt-hosted (the hosting
// fact dominates the display); otherwise it reports the lexically-first
// provisioner so the result is deterministic.
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
	for _, node := range cluster.Spec.Hosts {
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
