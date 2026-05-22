package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
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

func findClusterMachine(ci v1alpha1.ClusterInfra, name string) (v1alpha1.ClusterMachineComponent, bool) {
	for _, m := range ci.Spec.Components.Machines {
		if m.Name == name {
			return m, true
		}
	}
	return v1alpha1.ClusterMachineComponent{}, false
}

func resolveLoadBalancer(state v1alpha1.State, from v1alpha1.From) (v1alpha1.LoadBalancerCapability, bool) {
	p, ok := findProvider(state, from.Provider)
	if !ok {
		return v1alpha1.LoadBalancerCapability{}, false
	}
	return stateview.LoadBalancer(p, from.Name)
}

func resolveProxy(state v1alpha1.State, from v1alpha1.From) (v1alpha1.ProxyCapability, bool) {
	p, ok := findProvider(state, from.Provider)
	if !ok {
		return v1alpha1.ProxyCapability{}, false
	}
	return stateview.Proxy(p, from.Name)
}

func resolveDNS(state v1alpha1.State, from v1alpha1.From) (v1alpha1.DNSCapability, bool) {
	p, ok := findProvider(state, from.Provider)
	if !ok {
		return v1alpha1.DNSCapability{}, false
	}
	return stateview.DNS(p, from.Name)
}

func resolveRegistry(state v1alpha1.State, from v1alpha1.From) (v1alpha1.RegistryCapability, bool) {
	p, ok := findProvider(state, from.Provider)
	if !ok {
		return v1alpha1.RegistryCapability{}, false
	}
	return stateview.Registry(p, from.Name)
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

func clusterNameForCI(state v1alpha1.State, ci v1alpha1.ClusterInfra) string {
	if ocp, ok := clusterForCI(state, ci); ok {
		return ocp.Metadata.Name
	}
	return ci.Metadata.Name
}

func clusterForCI(state v1alpha1.State, ci v1alpha1.ClusterInfra) (v1alpha1.ContainerCluster, bool) {
	return stateview.ClusterForInfra(state, ci)
}
