package stategraph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

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

func FilterStateToApplyClusterRoots(state v1alpha1.State, containerNames, storageNames []string) v1alpha1.State {
	var parts []v1alpha1.State
	if len(containerNames) > 0 {
		parts = append(parts, FilterStateToClusters(state, containerNames))
	}
	if len(storageNames) > 0 {
		parts = append(parts, filterStateToStorageClustersForApply(state, storageNames))
	}
	if len(parts) == 0 {
		return state
	}
	return mergeFilteredStates(state, parts)
}

func FilterStateToClusters(state v1alpha1.State, names []string) v1alpha1.State {
	selectedOCP := nameSet(names)
	var filteredOCP []v1alpha1.ContainerCluster
	selectedMachines := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		if !selectedOCP[ocp.Metadata.Name] {
			continue
		}
		filteredOCP = append(filteredOCP, ocp)
		addContainerClusterMachines(selectedMachines, ocp)
	}
	addSelectedServiceMachines(selectedMachines, state, selectedOCP)
	state = filterStorageToClusters(state, selectedOCP, selectedMachines)
	state.ContainerClusters = filteredOCP
	state = filterMachinesAndProviders(state, selectedMachines)
	state = filterAddonsToClusters(state, selectedOCP)
	return state
}

func FilterStateToStorageClusters(state v1alpha1.State, names []string) v1alpha1.State {
	selected := nameSet(names)
	state = filterStorageObjects(state, selected)
	selectedMachines := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		addStorageClusterMachines(selectedMachines, cluster)
	}
	addSelectedServiceMachines(selectedMachines, state, selected)
	selectedContainerClusters := storageAttachmentContainerClusters(state)
	var containerClusters []v1alpha1.ContainerCluster
	for _, cluster := range state.ContainerClusters {
		if selectedContainerClusters[cluster.Metadata.Name] {
			containerClusters = append(containerClusters, cluster)
			addContainerClusterMachines(selectedMachines, cluster)
		}
	}
	state.ContainerClusters = containerClusters
	state = filterMachinesAndProviders(state, selectedMachines)
	state = filterAddonsToClusters(state, selectedContainerClusters)
	return state
}

func filterStateToStorageClustersForApply(state v1alpha1.State, names []string) v1alpha1.State {
	selected := nameSet(names)
	state = filterStorageObjects(state, selected)
	selectedMachines := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		addStorageClusterMachines(selectedMachines, cluster)
	}
	addSelectedServiceMachines(selectedMachines, state, selected)
	selectedBindingClusters := storageAttachmentContainerClusters(state)
	state.ContainerClusters = nil
	state = filterMachinesAndProviders(state, selectedMachines)
	state = filterAddonsToClusters(state, selectedBindingClusters)
	return state
}

func filterStorageObjects(state v1alpha1.State, selected map[string]bool) v1alpha1.State {
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
	var nfsExports []v1alpha1.StorageNFSExport
	for _, nfs := range state.StorageNFSExports {
		if selected[nfs.Spec.StorageClusterRef.Name] {
			nfsExports = append(nfsExports, nfs)
		}
	}
	var exports []v1alpha1.StorageExport
	for _, export := range state.StorageExports {
		if selected[export.Spec.StorageClusterRef.Name] {
			exports = append(exports, export)
		}
	}
	state.StorageClusters = clusters
	state.StoragePlacementPolicies = policies
	state.StoragePools = pools
	state.StorageFilesystems = filesystems
	state.StorageObjectGateways = gateways
	state.StorageNFSExports = nfsExports
	state.StorageExports = exports
	return state
}

func filterStorageToClusters(state v1alpha1.State, selectedClusters map[string]bool, selectedMachines map[string]bool) v1alpha1.State {
	selectedExports := map[string]bool{}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		if !selectedClusters[effect.Binding.Spec.ClusterRef.Name] {
			continue
		}
		selectedExports[addoninputs.LocalObjectReferenceValue(effect.Input).Name] = true
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
			selectedFilesystems[df.FilesystemRef.Name] = true
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
	var filteredClusters []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if !selectedStorageClusters[cluster.Metadata.Name] {
			continue
		}
		filteredClusters = append(filteredClusters, cluster)
		addStorageClusterMachines(selectedMachines, cluster)
	}
	var filteredNFSExports []v1alpha1.StorageNFSExport
	for _, nfs := range state.StorageNFSExports {
		if selectedStorageClusters[nfs.Spec.StorageClusterRef.Name] {
			filteredNFSExports = append(filteredNFSExports, nfs)
		}
	}
	state.StorageClusters = filteredClusters
	state.StoragePlacementPolicies = filteredPolicies
	state.StoragePools = filteredPools
	state.StorageFilesystems = filteredFilesystems
	state.StorageObjectGateways = filteredGateways
	state.StorageNFSExports = filteredNFSExports
	state.StorageExports = filteredExports
	return state
}

func storageAttachmentContainerClusters(state v1alpha1.State) map[string]bool {
	selectedExports := map[string]bool{}
	for _, export := range state.StorageExports {
		selectedExports[export.Metadata.Name] = true
	}
	selected := map[string]bool{}
	var bindings []v1alpha1.ClusterAddonBinding
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input)
		if !selectedExports[exportRef.Name] {
			continue
		}
		bindings = append(bindings, storageEffectBinding(effect))
		selected[effect.Binding.Spec.ClusterRef.Name] = true
	}
	state.ClusterAddonBindings = bindings
	return selected
}

func addContainerClusterMachines(out map[string]bool, cluster v1alpha1.ContainerCluster) {
	for _, node := range cluster.Spec.Nodes {
		if node.MachineRef.Name != "" {
			out[node.MachineRef.Name] = true
		}
	}
}

func addStorageClusterMachines(out map[string]bool, cluster v1alpha1.StorageCluster) {
	if cluster.Spec.Ceph == nil {
		return
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.MachineRef.Name != "" {
			out[node.MachineRef.Name] = true
		}
	}
}

func filterMachinesAndProviders(state v1alpha1.State, selectedMachines map[string]bool) v1alpha1.State {
	selectedProviders := map[string]bool{}
	for _, machine := range state.Machines {
		if !selectedMachines[machine.Metadata.Name] {
			continue
		}
		if machine.Spec.Substrate.ProviderRef.Name != "" {
			selectedProviders[machine.Spec.Substrate.ProviderRef.Name] = true
		}
	}
	var providers []v1alpha1.InfraProvider
	for _, provider := range state.InfraProviders {
		if !selectedProviders[provider.Metadata.Name] {
			continue
		}
		providers = append(providers, provider)
		if provider.Spec.Libvirt != nil && provider.Spec.Libvirt.MachineRef.Name != "" {
			selectedMachines[provider.Spec.Libvirt.MachineRef.Name] = true
		}
	}
	var machines []v1alpha1.Machine
	for _, machine := range state.Machines {
		if selectedMachines[machine.Metadata.Name] {
			machines = append(machines, machine)
		}
	}
	state.Machines = machines
	state.InfraProviders = providers
	return state
}

func addSelectedServiceMachines(out map[string]bool, state v1alpha1.State, selectedClusters map[string]bool) {
	for _, service := range ResolveMachineServices(state).Services {
		if service.MachineRef == "" {
			continue
		}
		for _, consumer := range service.Consumers {
			if selectedClusters[consumer.Cluster] {
				out[service.MachineRef] = true
				break
			}
		}
	}
}

func ApplyWorkObjects(state v1alpha1.State, containerNames, storageNames []string) (machines map[string]bool, storageClusters map[string]bool) {
	selectedContainer := nameSet(containerNames)
	selectedStorage := nameSet(storageNames)
	machines = map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		if selectedContainer[ocp.Metadata.Name] {
			addContainerClusterMachines(machines, ocp)
		}
	}
	for _, cluster := range state.StorageClusters {
		if selectedStorage[cluster.Metadata.Name] {
			addStorageClusterMachines(machines, cluster)
		}
	}
	selectedClusters := map[string]bool{}
	for name := range selectedContainer {
		selectedClusters[name] = true
	}
	for name := range selectedStorage {
		selectedClusters[name] = true
	}
	addSelectedServiceMachines(machines, state, selectedClusters)
	addProviderHostMachines(machines, state)
	return machines, selectedStorage
}

func addProviderHostMachines(machines map[string]bool, state v1alpha1.State) {
	providers := map[string]bool{}
	for _, machine := range state.Machines {
		if machines[machine.Metadata.Name] && machine.Spec.Substrate.ProviderRef.Name != "" {
			providers[machine.Spec.Substrate.ProviderRef.Name] = true
		}
	}
	for _, provider := range state.InfraProviders {
		if providers[provider.Metadata.Name] && provider.Spec.Libvirt != nil && provider.Spec.Libvirt.MachineRef.Name != "" {
			machines[provider.Spec.Libvirt.MachineRef.Name] = true
		}
	}
}

func filterAddonsToClusters(state v1alpha1.State, selectedClusters map[string]bool) v1alpha1.State {
	selectedSets := map[string]bool{}
	selectedAddons := map[string]bool{}
	var filteredBindings []v1alpha1.ClusterAddonBinding
	for _, binding := range state.ClusterAddonBindings {
		if !selectedClusters[binding.Spec.ClusterRef.Name] {
			continue
		}
		filteredBindings = append(filteredBindings, binding)
		for _, ref := range binding.Spec.ProfileRefs {
			selectedSets[ref.Name] = true
		}
		for _, addon := range addoninputs.EffectiveBindingAddons(state, binding) {
			selectedAddons[addon.AddonRef.Name] = true
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
		for _, ref := range set.Spec.ProfileRefs {
			if !selectedSets[ref.Name] {
				selectedSets[ref.Name] = true
				visitSet(ref.Name)
			}
		}
		for _, ref := range set.Spec.AddonRefs {
			selectedAddons[ref.Name] = true
		}
	}
	for name := range selectedSets {
		visitSet(name)
	}
	state.ClusterAddonBindings = filteredBindings
	state.ClusterAddonProfiles = filterByName(state.ClusterAddonProfiles, selectedSets, func(item v1alpha1.ClusterAddonProfile) string { return item.Metadata.Name })
	state.ClusterAddons = filterByName(state.ClusterAddons, selectedAddons, func(item v1alpha1.ClusterAddon) string { return item.Metadata.Name })
	return state
}

func storageEffectBinding(effect addoninputs.EffectBinding) v1alpha1.ClusterAddonBinding {
	return v1alpha1.ClusterAddonBinding{
		APIVersion: effect.Binding.APIVersion,
		Kind:       effect.Binding.Kind,
		Metadata:   effect.Binding.Metadata,
		Spec: v1alpha1.ClusterAddonBindingSpec{
			ClusterRef: effect.Binding.Spec.ClusterRef,
			AddonRefs:  []v1alpha1.LocalObjectReference{effect.Addon.AddonRef},
			AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
				AddonRef: effect.Addon.AddonRef,
				Inputs:   []v1alpha1.ClusterAddonBindingInput{effect.Input},
			}},
		},
		SourcePath: effect.Binding.SourcePath,
	}
}

func SelectedClusterForInstall(clusters []v1alpha1.ContainerCluster, infraName string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range clusters {
		if cluster.Metadata.Name == infraName {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func mergeFilteredStates(base v1alpha1.State, parts []v1alpha1.State) v1alpha1.State {
	out := base
	machines := map[string]bool{}
	infraProviders := map[string]bool{}
	containerClusters := map[string]bool{}
	storageClusters := map[string]bool{}
	storagePlacementPolicies := map[string]bool{}
	storagePools := map[string]bool{}
	storageFilesystems := map[string]bool{}
	storageObjectGateways := map[string]bool{}
	storageNFSExports := map[string]bool{}
	storageExports := map[string]bool{}
	clusterAddons := map[string]bool{}
	clusterAddonProfiles := map[string]bool{}
	clusterAddonBindings := map[string]bool{}
	for _, part := range parts {
		addNames(machines, part.Machines, func(item v1alpha1.Machine) string { return item.Metadata.Name })
		addNames(infraProviders, part.InfraProviders, func(item v1alpha1.InfraProvider) string { return item.Metadata.Name })
		addNames(containerClusters, part.ContainerClusters, func(item v1alpha1.ContainerCluster) string { return item.Metadata.Name })
		addNames(storageClusters, part.StorageClusters, func(item v1alpha1.StorageCluster) string { return item.Metadata.Name })
		addNames(storagePlacementPolicies, part.StoragePlacementPolicies, func(item v1alpha1.StoragePlacementPolicy) string { return item.Metadata.Name })
		addNames(storagePools, part.StoragePools, func(item v1alpha1.StoragePool) string { return item.Metadata.Name })
		addNames(storageFilesystems, part.StorageFilesystems, func(item v1alpha1.StorageFilesystem) string { return item.Metadata.Name })
		addNames(storageObjectGateways, part.StorageObjectGateways, func(item v1alpha1.StorageObjectGateway) string { return item.Metadata.Name })
		addNames(storageNFSExports, part.StorageNFSExports, func(item v1alpha1.StorageNFSExport) string { return item.Metadata.Name })
		addNames(storageExports, part.StorageExports, func(item v1alpha1.StorageExport) string { return item.Metadata.Name })
		addNames(clusterAddons, part.ClusterAddons, func(item v1alpha1.ClusterAddon) string { return item.Metadata.Name })
		addNames(clusterAddonProfiles, part.ClusterAddonProfiles, func(item v1alpha1.ClusterAddonProfile) string { return item.Metadata.Name })
		addNames(clusterAddonBindings, part.ClusterAddonBindings, func(item v1alpha1.ClusterAddonBinding) string { return item.Metadata.Name })
	}
	out.Machines = filterByName(base.Machines, machines, func(item v1alpha1.Machine) string { return item.Metadata.Name })
	out.InfraProviders = filterByName(base.InfraProviders, infraProviders, func(item v1alpha1.InfraProvider) string { return item.Metadata.Name })
	out.ContainerClusters = filterByName(base.ContainerClusters, containerClusters, func(item v1alpha1.ContainerCluster) string { return item.Metadata.Name })
	out.StorageClusters = filterByName(base.StorageClusters, storageClusters, func(item v1alpha1.StorageCluster) string { return item.Metadata.Name })
	out.StoragePlacementPolicies = filterByName(base.StoragePlacementPolicies, storagePlacementPolicies, func(item v1alpha1.StoragePlacementPolicy) string { return item.Metadata.Name })
	out.StoragePools = filterByName(base.StoragePools, storagePools, func(item v1alpha1.StoragePool) string { return item.Metadata.Name })
	out.StorageFilesystems = filterByName(base.StorageFilesystems, storageFilesystems, func(item v1alpha1.StorageFilesystem) string { return item.Metadata.Name })
	out.StorageObjectGateways = filterByName(base.StorageObjectGateways, storageObjectGateways, func(item v1alpha1.StorageObjectGateway) string { return item.Metadata.Name })
	out.StorageNFSExports = filterByName(base.StorageNFSExports, storageNFSExports, func(item v1alpha1.StorageNFSExport) string { return item.Metadata.Name })
	out.StorageExports = filterByName(base.StorageExports, storageExports, func(item v1alpha1.StorageExport) string { return item.Metadata.Name })
	out.ClusterAddons = filterByName(base.ClusterAddons, clusterAddons, func(item v1alpha1.ClusterAddon) string { return item.Metadata.Name })
	out.ClusterAddonProfiles = filterByName(base.ClusterAddonProfiles, clusterAddonProfiles, func(item v1alpha1.ClusterAddonProfile) string { return item.Metadata.Name })
	out.ClusterAddonBindings = filterByName(base.ClusterAddonBindings, clusterAddonBindings, func(item v1alpha1.ClusterAddonBinding) string { return item.Metadata.Name })
	return out
}

func nameSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		out[name] = true
	}
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
