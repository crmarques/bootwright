package v1alpha1

// KindAccessor ties one authored kind to its State list so cross-kind walks
// (resource sets, presence checks, duplicate-name scans) iterate one table
// instead of hand-enumerating kinds. kinds_test.go proves the table covers
// every State field exactly once, and the loader probe test in
// internal/state/desired proves every kind round-trips through Load, so
// registering a new kind means adding the State field, the Kind constant, one
// accessor here, and the loadFile decode case — the guards fail on any subset.
type KindAccessor struct {
	// Kind is the authored kind discriminator (the Kind* constant).
	Kind string
	// StateField names the State field holding this kind's objects; the
	// completeness test matches it against the State struct by reflection.
	StateField string
	// Names returns metadata.name for every loaded object of this kind, in
	// declaration order.
	Names func(State) []string
}

// AuthoredKindAccessors lists every authored kind in State declaration order.
func AuthoredKindAccessors() []KindAccessor {
	return []KindAccessor{
		{KindEnvironment, "Environments", func(s State) []string {
			return metadataNames(s.Environments, func(o Environment) Metadata { return o.Metadata })
		}},
		{KindEntitlement, "Entitlements", func(s State) []string {
			return metadataNames(s.Entitlements, func(o Entitlement) Metadata { return o.Metadata })
		}},
		{KindMachine, "Machines", func(s State) []string {
			return metadataNames(s.Machines, func(o Machine) Metadata { return o.Metadata })
		}},
		{KindMachineImage, "MachineImages", func(s State) []string {
			return metadataNames(s.MachineImages, func(o MachineImage) Metadata { return o.Metadata })
		}},
		{KindMachineInstallProfile, "MachineInstallProfiles", func(s State) []string {
			return metadataNames(s.MachineInstallProfiles, func(o MachineInstallProfile) Metadata { return o.Metadata })
		}},
		{KindNetworkConfig, "NetworkConfigs", func(s State) []string {
			return metadataNames(s.NetworkConfigs, func(o NetworkConfig) Metadata { return o.Metadata })
		}},
		{KindInfraProvider, "InfraProviders", func(s State) []string {
			return metadataNames(s.InfraProviders, func(o InfraProvider) Metadata { return o.Metadata })
		}},
		{KindInfraComponent, "InfraComponents", func(s State) []string {
			return metadataNames(s.InfraComponents, func(o InfraComponent) Metadata { return o.Metadata })
		}},
		{KindContainerCluster, "ContainerClusters", func(s State) []string {
			return metadataNames(s.ContainerClusters, func(o ContainerCluster) Metadata { return o.Metadata })
		}},
		{KindStorageCluster, "StorageClusters", func(s State) []string {
			return metadataNames(s.StorageClusters, func(o StorageCluster) Metadata { return o.Metadata })
		}},
		{KindStoragePlacementPolicy, "StoragePlacementPolicies", func(s State) []string {
			return metadataNames(s.StoragePlacementPolicies, func(o StoragePlacementPolicy) Metadata { return o.Metadata })
		}},
		{KindStoragePool, "StoragePools", func(s State) []string {
			return metadataNames(s.StoragePools, func(o StoragePool) Metadata { return o.Metadata })
		}},
		{KindStorageFilesystem, "StorageFilesystems", func(s State) []string {
			return metadataNames(s.StorageFilesystems, func(o StorageFilesystem) Metadata { return o.Metadata })
		}},
		{KindStorageObjectGateway, "StorageObjectGateways", func(s State) []string {
			return metadataNames(s.StorageObjectGateways, func(o StorageObjectGateway) Metadata { return o.Metadata })
		}},
		{KindStorageNFSExport, "StorageNFSExports", func(s State) []string {
			return metadataNames(s.StorageNFSExports, func(o StorageNFSExport) Metadata { return o.Metadata })
		}},
		{KindStorageExport, "StorageExports", func(s State) []string {
			return metadataNames(s.StorageExports, func(o StorageExport) Metadata { return o.Metadata })
		}},
		{KindClusterAddon, "ClusterAddons", func(s State) []string {
			return metadataNames(s.ClusterAddons, func(o ClusterAddon) Metadata { return o.Metadata })
		}},
		{KindClusterAddonProfile, "ClusterAddonProfiles", func(s State) []string {
			return metadataNames(s.ClusterAddonProfiles, func(o ClusterAddonProfile) Metadata { return o.Metadata })
		}},
		{KindClusterAddonBinding, "ClusterAddonBindings", func(s State) []string {
			return metadataNames(s.ClusterAddonBindings, func(o ClusterAddonBinding) Metadata { return o.Metadata })
		}},
		{KindProvisioningPlaybook, "ProvisioningPlaybooks", func(s State) []string {
			return metadataNames(s.ProvisioningPlaybooks, func(o ProvisioningPlaybook) Metadata { return o.Metadata })
		}},
	}
}

func metadataNames[T any](items []T, meta func(T) Metadata) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = meta(item).Name
	}
	return out
}
