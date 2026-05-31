package stategraph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// FilterStateToClusterRoots keeps the graph closure for the selected root
// container and storage clusters. An empty root family contributes no roots;
// callers should skip this filter entirely when both families are empty.
func FilterStateToClusterRoots(state v1alpha1.State, containerNames, storageNames []string) v1alpha1.State {
	var parts []v1alpha1.State
	if len(containerNames) > 0 {
		parts = append(parts, FilterStateToClusters(state, containerNames))
	}
	if len(storageNames) > 0 {
		parts = append(parts, FilterStateToStorageClusters(state, storageNames))
	}
	if len(parts) == 0 {
		return state
	}
	return mergeFilteredStates(state, parts)
}

// FilterStateToClusters keeps selected ContainerClusters, their referenced
// ClusterInfras, and the providers needed by those ClusterInfras.
func FilterStateToClusters(state v1alpha1.State, names []string) v1alpha1.State {
	selectedOCP := map[string]bool{}
	for _, name := range names {
		selectedOCP[name] = true
	}
	selectedInfra := map[string]bool{}
	filteredOCP := make([]v1alpha1.ContainerCluster, 0, len(names))
	for _, ocp := range state.ContainerClusters {
		if !selectedOCP[ocp.Metadata.Name] {
			continue
		}
		filteredOCP = append(filteredOCP, ocp)
		for _, ref := range stateview.ClusterInfraNames(ocp) {
			selectedInfra[ref] = true
		}
	}
	state, storageInfra := filterStorageToClusters(state, selectedOCP)
	for name := range storageInfra {
		selectedInfra[name] = true
	}

	selectedProviders := map[string]bool{}
	filteredInfra := make([]v1alpha1.ClusterInfra, 0, len(state.ClusterInfras))
	for _, infra := range state.ClusterInfras {
		if !selectedInfra[infra.Metadata.Name] {
			continue
		}
		filteredInfra = append(filteredInfra, infra)
		for _, m := range infra.Spec.Components.Machines {
			if m.From.Provider != "" {
				selectedProviders[m.From.Provider] = true
			}
		}
	}

	filteredProviders := make([]v1alpha1.InfraProvider, 0, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		if selectedProviders[provider.Metadata.Name] {
			filteredProviders = append(filteredProviders, provider)
		}
	}

	state.InfraProviders = filteredProviders
	state.ClusterInfras = filteredInfra
	state.ContainerClusters = filteredOCP
	state = filterAddonsToClusters(state, selectedOCP)
	return state
}

func FilterStateToStorageClusters(state v1alpha1.State, names []string) v1alpha1.State {
	selected := map[string]bool{}
	for _, name := range names {
		selected[name] = true
	}
	var clusters []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if selected[cluster.Metadata.Name] {
			clusters = append(clusters, cluster)
		}
	}
	var policies []v1alpha1.StoragePlacementPolicy
	for _, policy := range state.StoragePlacementPolicies {
		if selected[policy.Spec.StorageClusterRef.Name] {
			policies = append(policies, policy)
		}
	}
	var pools []v1alpha1.StoragePool
	for _, pool := range state.StoragePools {
		if selected[pool.Spec.StorageClusterRef.Name] {
			pools = append(pools, pool)
		}
	}
	var filesystems []v1alpha1.StorageFilesystem
	for _, fs := range state.StorageFilesystems {
		if selected[fs.Spec.StorageClusterRef.Name] {
			filesystems = append(filesystems, fs)
		}
	}
	var gateways []v1alpha1.StorageObjectGateway
	for _, gateway := range state.StorageObjectGateways {
		if selected[gateway.Spec.StorageClusterRef.Name] {
			gateways = append(gateways, gateway)
		}
	}
	var exports []v1alpha1.StorageExport
	selectedExports := map[string]bool{}
	for _, export := range state.StorageExports {
		if selected[export.Spec.StorageClusterRef.Name] {
			exports = append(exports, export)
			selectedExports[export.Metadata.Name] = true
		}
	}
	var bindings []v1alpha1.StorageClusterBinding
	selectedContainerClusters := map[string]bool{}
	for _, binding := range state.StorageClusterBindings {
		if !selectedExports[binding.Spec.StorageExportRef.Name] {
			continue
		}
		bindings = append(bindings, binding)
		for _, name := range binding.Spec.ContainerClusterSelector.Names {
			selectedContainerClusters[name] = true
		}
	}
	var containerClusters []v1alpha1.ContainerCluster
	for _, cluster := range state.ContainerClusters {
		if selectedContainerClusters[cluster.Metadata.Name] {
			containerClusters = append(containerClusters, cluster)
		}
	}
	selectedInfra := map[string]bool{}
	for _, cluster := range clusters {
		if cluster.Spec.ClusterInfraRef.Name != "" {
			selectedInfra[cluster.Spec.ClusterInfraRef.Name] = true
		}
	}
	var infras []v1alpha1.ClusterInfra
	selectedProviders := map[string]bool{}
	for _, infra := range state.ClusterInfras {
		if !selectedInfra[infra.Metadata.Name] {
			continue
		}
		infras = append(infras, infra)
		for _, machine := range infra.Spec.Components.Machines {
			if machine.From.Provider != "" {
				selectedProviders[machine.From.Provider] = true
			}
		}
	}
	var providers []v1alpha1.InfraProvider
	for _, provider := range state.InfraProviders {
		if selectedProviders[provider.Metadata.Name] {
			providers = append(providers, provider)
		}
	}
	state.InfraProviders = providers
	state.ClusterInfras = infras
	state.ContainerClusters = containerClusters
	state.StorageClusters = clusters
	state.StoragePlacementPolicies = policies
	state.StoragePools = pools
	state.StorageFilesystems = filesystems
	state.StorageObjectGateways = gateways
	state.StorageExports = exports
	state.StorageClusterBindings = bindings
	state = filterAddonsToClusters(state, selectedContainerClusters)
	return state
}

func filterAddonsToClusters(state v1alpha1.State, selectedClusters map[string]bool) v1alpha1.State {
	selectedSets := map[string]bool{}
	selectedAddons := map[string]bool{}
	var filteredBindings []v1alpha1.ClusterAddonBinding
	for _, binding := range state.ClusterAddonBindings {
		var names []string
		for _, name := range binding.Spec.ContainerClusterSelector.Names {
			if selectedClusters[name] {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		binding.Spec.ContainerClusterSelector.Names = names
		filteredBindings = append(filteredBindings, binding)
		for _, ref := range binding.Spec.Profiles {
			selectedSets[ref.Name] = true
		}
		for _, ref := range binding.Spec.Addons {
			selectedAddons[ref.Name] = true
		}
	}
	setByName := map[string]v1alpha1.ClusterAddonProfile{}
	for _, set := range state.ClusterAddonProfiles {
		setByName[set.Metadata.Name] = set
	}
	var visitSet func(string)
	visitSet = func(name string) {
		set, ok := setByName[name]
		if !ok {
			return
		}
		for _, ref := range set.Spec.Profiles {
			if !selectedSets[ref.Name] {
				selectedSets[ref.Name] = true
				visitSet(ref.Name)
			}
		}
		for _, ref := range set.Spec.Addons {
			selectedAddons[ref.Name] = true
		}
	}
	for name := range selectedSets {
		visitSet(name)
	}
	var filteredSets []v1alpha1.ClusterAddonProfile
	for _, set := range state.ClusterAddonProfiles {
		if selectedSets[set.Metadata.Name] {
			filteredSets = append(filteredSets, set)
		}
	}
	var filteredAddons []v1alpha1.ClusterAddon
	for _, extension := range state.ClusterAddons {
		if selectedAddons[extension.Metadata.Name] {
			filteredAddons = append(filteredAddons, extension)
		}
	}
	state.ClusterAddonBindings = filteredBindings
	state.ClusterAddonProfiles = filteredSets
	state.ClusterAddons = filteredAddons
	return state
}

func filterStorageToClusters(state v1alpha1.State, selectedClusters map[string]bool) (v1alpha1.State, map[string]bool) {
	selectedExports := map[string]bool{}
	var filteredBindings []v1alpha1.StorageClusterBinding
	for _, binding := range state.StorageClusterBindings {
		var names []string
		for _, name := range binding.Spec.ContainerClusterSelector.Names {
			if selectedClusters[name] {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		binding.Spec.ContainerClusterSelector.Names = names
		filteredBindings = append(filteredBindings, binding)
		selectedExports[binding.Spec.StorageExportRef.Name] = true
	}

	selectedStorageClusters := map[string]bool{}
	selectedPools := map[string]bool{}
	selectedFilesystems := map[string]bool{}
	selectedGateways := map[string]bool{}
	var filteredExports []v1alpha1.StorageExport
	for _, export := range state.StorageExports {
		if !selectedExports[export.Metadata.Name] {
			continue
		}
		filteredExports = append(filteredExports, export)
		selectedStorageClusters[export.Spec.StorageClusterRef.Name] = true
		if df := export.Spec.DataFoundation; df != nil {
			selectedPools[df.RBDPoolRef.Name] = true
			selectedFilesystems[df.CephFSRef.Name] = true
			selectedGateways[df.ObjectGatewayRef.Name] = true
		}
	}

	filesystemByName := map[string]v1alpha1.StorageFilesystem{}
	for _, fs := range state.StorageFilesystems {
		filesystemByName[fs.Metadata.Name] = fs
	}
	for name := range selectedFilesystems {
		fs, ok := filesystemByName[name]
		if !ok {
			continue
		}
		selectedStorageClusters[fs.Spec.StorageClusterRef.Name] = true
		selectedPools[fs.Spec.CephFS.MetadataPoolRef.Name] = true
		for _, ref := range fs.Spec.CephFS.DataPoolRefs {
			selectedPools[ref.Name] = true
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if selectedGateways[gw.Metadata.Name] {
			selectedStorageClusters[gw.Spec.StorageClusterRef.Name] = true
		}
	}

	selectedPolicies := map[string]bool{}
	var filteredPools []v1alpha1.StoragePool
	for _, pool := range state.StoragePools {
		if !selectedPools[pool.Metadata.Name] {
			continue
		}
		filteredPools = append(filteredPools, pool)
		selectedStorageClusters[pool.Spec.StorageClusterRef.Name] = true
		selectedPolicies[pool.Spec.PlacementPolicyRef.Name] = true
	}

	var filteredFilesystems []v1alpha1.StorageFilesystem
	for _, fs := range state.StorageFilesystems {
		if selectedFilesystems[fs.Metadata.Name] {
			filteredFilesystems = append(filteredFilesystems, fs)
		}
	}
	var filteredGateways []v1alpha1.StorageObjectGateway
	for _, gw := range state.StorageObjectGateways {
		if selectedGateways[gw.Metadata.Name] {
			filteredGateways = append(filteredGateways, gw)
		}
	}
	var filteredPolicies []v1alpha1.StoragePlacementPolicy
	for _, policy := range state.StoragePlacementPolicies {
		if selectedPolicies[policy.Metadata.Name] {
			filteredPolicies = append(filteredPolicies, policy)
		}
	}

	storageInfra := map[string]bool{}
	var filteredClusters []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if !selectedStorageClusters[cluster.Metadata.Name] {
			continue
		}
		filteredClusters = append(filteredClusters, cluster)
		if cluster.Spec.ClusterInfraRef.Name != "" {
			storageInfra[cluster.Spec.ClusterInfraRef.Name] = true
		}
	}

	state.StorageClusters = filteredClusters
	state.StoragePlacementPolicies = filteredPolicies
	state.StoragePools = filteredPools
	state.StorageFilesystems = filteredFilesystems
	state.StorageObjectGateways = filteredGateways
	state.StorageExports = filteredExports
	state.StorageClusterBindings = filteredBindings
	return state, storageInfra
}

func SelectedClusterForInfra(clusters []v1alpha1.ContainerCluster, infraName string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range clusters {
		for _, ref := range stateview.ClusterInfraNames(cluster) {
			if ref == infraName {
				return cluster, true
			}
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func ClusterInfraNames(ocp v1alpha1.ContainerCluster) []string {
	return stateview.ClusterInfraNames(ocp)
}

func mergeFilteredStates(base v1alpha1.State, parts []v1alpha1.State) v1alpha1.State {
	out := base
	infraProviders := map[string]bool{}
	clusterInfras := map[string]bool{}
	containerClusters := map[string]bool{}
	storageClusters := map[string]bool{}
	storagePlacementPolicies := map[string]bool{}
	storagePools := map[string]bool{}
	storageFilesystems := map[string]bool{}
	storageObjectGateways := map[string]bool{}
	storageExports := map[string]bool{}
	storageClusterBindings := map[string]bool{}
	clusterAddons := map[string]bool{}
	clusterAddonProfiles := map[string]bool{}
	clusterAddonBindings := map[string]bool{}
	for _, part := range parts {
		addNames(infraProviders, part.InfraProviders, func(item v1alpha1.InfraProvider) string { return item.Metadata.Name })
		addNames(clusterInfras, part.ClusterInfras, func(item v1alpha1.ClusterInfra) string { return item.Metadata.Name })
		addNames(containerClusters, part.ContainerClusters, func(item v1alpha1.ContainerCluster) string { return item.Metadata.Name })
		addNames(storageClusters, part.StorageClusters, func(item v1alpha1.StorageCluster) string { return item.Metadata.Name })
		addNames(storagePlacementPolicies, part.StoragePlacementPolicies, func(item v1alpha1.StoragePlacementPolicy) string { return item.Metadata.Name })
		addNames(storagePools, part.StoragePools, func(item v1alpha1.StoragePool) string { return item.Metadata.Name })
		addNames(storageFilesystems, part.StorageFilesystems, func(item v1alpha1.StorageFilesystem) string { return item.Metadata.Name })
		addNames(storageObjectGateways, part.StorageObjectGateways, func(item v1alpha1.StorageObjectGateway) string { return item.Metadata.Name })
		addNames(storageExports, part.StorageExports, func(item v1alpha1.StorageExport) string { return item.Metadata.Name })
		addNames(storageClusterBindings, part.StorageClusterBindings, func(item v1alpha1.StorageClusterBinding) string { return item.Metadata.Name })
		addNames(clusterAddons, part.ClusterAddons, func(item v1alpha1.ClusterAddon) string { return item.Metadata.Name })
		addNames(clusterAddonProfiles, part.ClusterAddonProfiles, func(item v1alpha1.ClusterAddonProfile) string { return item.Metadata.Name })
		addNames(clusterAddonBindings, part.ClusterAddonBindings, func(item v1alpha1.ClusterAddonBinding) string { return item.Metadata.Name })
	}
	out.InfraProviders = filterByName(base.InfraProviders, infraProviders, func(item v1alpha1.InfraProvider) string { return item.Metadata.Name })
	out.ClusterInfras = filterByName(base.ClusterInfras, clusterInfras, func(item v1alpha1.ClusterInfra) string { return item.Metadata.Name })
	out.ContainerClusters = filterByName(base.ContainerClusters, containerClusters, func(item v1alpha1.ContainerCluster) string { return item.Metadata.Name })
	out.StorageClusters = filterByName(base.StorageClusters, storageClusters, func(item v1alpha1.StorageCluster) string { return item.Metadata.Name })
	out.StoragePlacementPolicies = filterByName(base.StoragePlacementPolicies, storagePlacementPolicies, func(item v1alpha1.StoragePlacementPolicy) string { return item.Metadata.Name })
	out.StoragePools = filterByName(base.StoragePools, storagePools, func(item v1alpha1.StoragePool) string { return item.Metadata.Name })
	out.StorageFilesystems = filterByName(base.StorageFilesystems, storageFilesystems, func(item v1alpha1.StorageFilesystem) string { return item.Metadata.Name })
	out.StorageObjectGateways = filterByName(base.StorageObjectGateways, storageObjectGateways, func(item v1alpha1.StorageObjectGateway) string { return item.Metadata.Name })
	out.StorageExports = filterByName(base.StorageExports, storageExports, func(item v1alpha1.StorageExport) string { return item.Metadata.Name })
	out.StorageClusterBindings = filterByName(base.StorageClusterBindings, storageClusterBindings, func(item v1alpha1.StorageClusterBinding) string { return item.Metadata.Name })
	out.ClusterAddons = filterByName(base.ClusterAddons, clusterAddons, func(item v1alpha1.ClusterAddon) string { return item.Metadata.Name })
	out.ClusterAddonProfiles = filterByName(base.ClusterAddonProfiles, clusterAddonProfiles, func(item v1alpha1.ClusterAddonProfile) string { return item.Metadata.Name })
	out.ClusterAddonBindings = filterByName(base.ClusterAddonBindings, clusterAddonBindings, func(item v1alpha1.ClusterAddonBinding) string { return item.Metadata.Name })
	return out
}

func addNames[T any](out map[string]bool, items []T, name func(T) string) {
	for _, item := range items {
		out[name(item)] = true
	}
}

func filterByName[T any](items []T, selected map[string]bool, name func(T) string) []T {
	out := make([]T, 0, len(selected))
	for _, item := range items {
		if selected[name(item)] {
			out = append(out, item)
		}
	}
	return out
}
