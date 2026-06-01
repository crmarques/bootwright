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

func lookupNetworkAttachment(provider v1alpha1.InfraProvider, name string) (v1alpha1.NetworkAttachmentCapability, bool) {
	for _, attachment := range provider.Spec.NetworkAttachments {
		if attachment.Name == name {
			return attachment, true
		}
	}
	return v1alpha1.NetworkAttachmentCapability{}, false
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

func indexClusterAddons(items []v1alpha1.ClusterAddon) map[string]v1alpha1.ClusterAddon {
	out := make(map[string]v1alpha1.ClusterAddon, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func indexClusterAddonProfiles(items []v1alpha1.ClusterAddonProfile) map[string]v1alpha1.ClusterAddonProfile {
	out := make(map[string]v1alpha1.ClusterAddonProfile, len(items))
	for _, c := range items {
		out[c.Metadata.Name] = c
	}
	return out
}

func indexStorageClusters(items []v1alpha1.StorageCluster) map[string]v1alpha1.StorageCluster {
	out := make(map[string]v1alpha1.StorageCluster, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexStoragePlacementPolicies(items []v1alpha1.StoragePlacementPolicy) map[string]v1alpha1.StoragePlacementPolicy {
	out := make(map[string]v1alpha1.StoragePlacementPolicy, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexStoragePools(items []v1alpha1.StoragePool) map[string]v1alpha1.StoragePool {
	out := make(map[string]v1alpha1.StoragePool, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexStorageFilesystems(items []v1alpha1.StorageFilesystem) map[string]v1alpha1.StorageFilesystem {
	out := make(map[string]v1alpha1.StorageFilesystem, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexStorageObjectGateways(items []v1alpha1.StorageObjectGateway) map[string]v1alpha1.StorageObjectGateway {
	out := make(map[string]v1alpha1.StorageObjectGateway, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexStorageExports(items []v1alpha1.StorageExport) map[string]v1alpha1.StorageExport {
	out := make(map[string]v1alpha1.StorageExport, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
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
