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

func indexMachines(machines []v1alpha1.Machine) map[string]v1alpha1.Machine {
	out := make(map[string]v1alpha1.Machine, len(machines))
	for _, m := range machines {
		out[m.Metadata.Name] = m
	}
	return out
}

func indexMachineImages(items []v1alpha1.MachineImage) map[string]v1alpha1.MachineImage {
	out := make(map[string]v1alpha1.MachineImage, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func indexMachineInstallProfiles(items []v1alpha1.MachineInstallProfile) map[string]v1alpha1.MachineInstallProfile {
	out := make(map[string]v1alpha1.MachineInstallProfile, len(items))
	for _, item := range items {
		out[item.Metadata.Name] = item
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

func providerNetworkAttachmentNames(provider v1alpha1.InfraProvider) []string {
	var names []string
	for _, attachment := range provider.Spec.NetworkAttachments {
		if v1alpha1.NetworkAttachmentKind(attachment) == provider.Spec.Type {
			names = append(names, attachment.Name)
		}
	}
	return names
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

func lookupMachineProfile(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfile, bool) {
	return stateview.MachineProfile(p, name)
}

func machineHasCapability(m v1alpha1.Machine, want string) bool {
	return stateview.MachineHasCapability(m, want)
}
