package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Lookup helpers shared by installer, vars, and inventory.

func findProvider(state v1alpha1.State, name string) (v1alpha1.InfraProvider, bool) {
	return stateview.Provider(state, name)
}

func findNetworkConfig(state v1alpha1.State, name string) (v1alpha1.NetworkConfig, bool) {
	return stateview.NetworkConfig(state, name)
}

func findHost(state v1alpha1.State, name string) (v1alpha1.Host, bool) {
	return stateview.Host(state, name)
}

func lookupHostAddress(state v1alpha1.State, name string) string {
	if h, ok := findHost(state, name); ok && h.Spec.SSH != nil {
		return v1alpha1.HostSSHAddress(h)
	}
	return ""
}

func findProfile(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfileCapability, bool) {
	return stateview.MachineProfile(p, name)
}

func findProviderMachine(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineCapability, bool) {
	return stateview.Machine(p, name)
}

func findNetworkAttachment(p v1alpha1.InfraProvider, name string) (v1alpha1.NetworkAttachmentCapability, bool) {
	for _, attachment := range p.Spec.NetworkAttachments {
		if attachment.Name == name {
			return attachment, true
		}
	}
	return v1alpha1.NetworkAttachmentCapability{}, false
}

func findClusterNetworkBinding(ci v1alpha1.ClusterInfra, providerName, networkName string) (v1alpha1.ClusterNetworkBinding, bool) {
	for _, binding := range ci.Spec.NetworkBindings {
		if binding.ProviderRef.Name == providerName && binding.NetworkConfigRef.Name == networkName {
			return binding, true
		}
	}
	return v1alpha1.ClusterNetworkBinding{}, false
}

func findClusterMachine(ci v1alpha1.ClusterInfra, name string) (v1alpha1.ClusterMachineComponent, bool) {
	for _, m := range ci.Spec.Components.Machines {
		if m.Name == name {
			return m, true
		}
	}
	return v1alpha1.ClusterMachineComponent{}, false
}

func primaryEnvironment(state v1alpha1.State) *v1alpha1.Environment {
	return stateview.Environment(state)
}

func sortedNodes(nodes []v1alpha1.OCPNodeSpec) []v1alpha1.OCPNodeSpec {
	out := append([]v1alpha1.OCPNodeSpec(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Hostname < out[j].Hostname
	})
	return out
}

// clusterNodesForCI returns the ContainerCluster.spec.nodes map for the
// ContainerCluster bound to this ClusterInfra, or nil when no binding
// exists.
func clusterNodesForCI(state v1alpha1.State, ci v1alpha1.ClusterInfra) map[string]v1alpha1.OCPNodeSpec {
	return stateview.ClusterNodesForInfra(state, ci)
}

func clusterForCI(state v1alpha1.State, ci v1alpha1.ClusterInfra) (v1alpha1.ContainerCluster, bool) {
	return stateview.ClusterForInfra(state, ci)
}
