package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Lookup helpers shared by normalize and validate. Pure functions —
// no error reporting, no defaults. Callers handle missing values.

func indexProviders(providers []v1alpha1.InfraProvider) map[string]v1alpha1.InfraProvider {
	out := make(map[string]v1alpha1.InfraProvider, len(providers))
	for _, p := range providers {
		out[p.Metadata.Name] = p
	}
	return out
}

func indexHosts(hosts []v1alpha1.Host) map[string]v1alpha1.Host {
	out := make(map[string]v1alpha1.Host, len(hosts))
	for _, h := range hosts {
		out[h.Metadata.Name] = h
	}
	return out
}

func indexNetworkConfigs(nets []v1alpha1.NetworkConfig) map[string]v1alpha1.NetworkConfig {
	out := make(map[string]v1alpha1.NetworkConfig, len(nets))
	for _, n := range nets {
		out[n.Metadata.Name] = n
	}
	return out
}

func indexClusterInfras(items []v1alpha1.ClusterInfra) map[string]v1alpha1.ClusterInfra {
	out := make(map[string]v1alpha1.ClusterInfra, len(items))
	for _, ci := range items {
		out[ci.Metadata.Name] = ci
	}
	return out
}

func indexInfraComponents(items []v1alpha1.InfraComponent) map[string]v1alpha1.InfraComponent {
	out := make(map[string]v1alpha1.InfraComponent, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func indexContainerClusters(items []v1alpha1.ContainerCluster) map[string]v1alpha1.ContainerCluster {
	out := make(map[string]v1alpha1.ContainerCluster, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func indexClusterExtensions(items []v1alpha1.ClusterExtension) map[string]v1alpha1.ClusterExtension {
	out := make(map[string]v1alpha1.ClusterExtension, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func indexClusterExtensionSets(items []v1alpha1.ClusterExtensionSet) map[string]v1alpha1.ClusterExtensionSet {
	out := make(map[string]v1alpha1.ClusterExtensionSet, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func lookupMachineProfile(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfileCapability, bool) {
	return stateview.MachineProfile(p, name)
}

func lookupMachine(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineCapability, bool) {
	return stateview.Machine(p, name)
}

func hostHasCapability(h v1alpha1.Host, want string) bool {
	return stateview.HostHasCapability(h, want)
}
