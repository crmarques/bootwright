package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Lookup helpers shared by normalize and validate. Pure functions —
// no error reporting, no defaults. Callers handle missing values.

// indexByName builds a name->item map. It is the single owner of the
// by-Metadata.Name index that every kind needs; the per-kind helpers below are
// thin typed entry points over it rather than copy-pasted map builders.
func indexByName[T any](items []T, name func(T) string) map[string]T {
	out := make(map[string]T, len(items))
	for _, item := range items {
		out[name(item)] = item
	}
	return out
}

func indexProviders(items []v1alpha1.InfraProvider) map[string]v1alpha1.InfraProvider {
	return indexByName(items, func(p v1alpha1.InfraProvider) string { return p.Metadata.Name })
}

func indexMachines(items []v1alpha1.Machine) map[string]v1alpha1.Machine {
	return indexByName(items, func(m v1alpha1.Machine) string { return m.Metadata.Name })
}

func indexMachineImages(items []v1alpha1.MachineImage) map[string]v1alpha1.MachineImage {
	return indexByName(items, func(i v1alpha1.MachineImage) string { return i.Metadata.Name })
}

func indexMachineInstallProfiles(items []v1alpha1.MachineInstallProfile) map[string]v1alpha1.MachineInstallProfile {
	return indexByName(items, func(i v1alpha1.MachineInstallProfile) string { return i.Metadata.Name })
}

func indexNetworkConfigs(items []v1alpha1.NetworkConfig) map[string]v1alpha1.NetworkConfig {
	return indexByName(items, func(n v1alpha1.NetworkConfig) string { return n.Metadata.Name })
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
	return indexByName(items, func(c v1alpha1.InfraComponent) string { return c.Metadata.Name })
}

func indexContainerClusters(items []v1alpha1.ContainerCluster) map[string]v1alpha1.ContainerCluster {
	return indexByName(items, func(c v1alpha1.ContainerCluster) string { return c.Metadata.Name })
}

func indexClusterAddons(items []v1alpha1.ClusterAddon) map[string]v1alpha1.ClusterAddon {
	return indexByName(items, func(c v1alpha1.ClusterAddon) string { return c.Metadata.Name })
}

func indexClusterAddonProfiles(items []v1alpha1.ClusterAddonProfile) map[string]v1alpha1.ClusterAddonProfile {
	return indexByName(items, func(c v1alpha1.ClusterAddonProfile) string { return c.Metadata.Name })
}

func indexStorageClusters(items []v1alpha1.StorageCluster) map[string]v1alpha1.StorageCluster {
	return indexByName(items, func(c v1alpha1.StorageCluster) string { return c.Metadata.Name })
}

func indexStoragePlacementPolicies(items []v1alpha1.StoragePlacementPolicy) map[string]v1alpha1.StoragePlacementPolicy {
	return indexByName(items, func(p v1alpha1.StoragePlacementPolicy) string { return p.Metadata.Name })
}

func indexStoragePools(items []v1alpha1.StoragePool) map[string]v1alpha1.StoragePool {
	return indexByName(items, func(p v1alpha1.StoragePool) string { return p.Metadata.Name })
}

func indexStorageFilesystems(items []v1alpha1.StorageFilesystem) map[string]v1alpha1.StorageFilesystem {
	return indexByName(items, func(f v1alpha1.StorageFilesystem) string { return f.Metadata.Name })
}

func indexStorageObjectGateways(items []v1alpha1.StorageObjectGateway) map[string]v1alpha1.StorageObjectGateway {
	return indexByName(items, func(g v1alpha1.StorageObjectGateway) string { return g.Metadata.Name })
}

func indexStorageExports(items []v1alpha1.StorageExport) map[string]v1alpha1.StorageExport {
	return indexByName(items, func(e v1alpha1.StorageExport) string { return e.Metadata.Name })
}

func lookupMachineProfile(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfile, bool) {
	return stateview.MachineProfile(p, name)
}

func machineHasCapability(m v1alpha1.Machine, want string) bool {
	return stateview.MachineHasCapability(m, want)
}
