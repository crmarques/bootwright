package stategraph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

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
	state = filterExtensionsToClusters(state, selectedOCP)
	return state
}

func filterExtensionsToClusters(state v1alpha1.State, selectedClusters map[string]bool) v1alpha1.State {
	selectedSets := map[string]bool{}
	selectedExtensions := map[string]bool{}
	var filteredBindings []v1alpha1.ClusterExtensionBinding
	for _, binding := range state.ClusterExtensionBindings {
		var names []string
		for _, name := range binding.Spec.ClusterSelector.Names {
			if selectedClusters[name] {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		binding.Spec.ClusterSelector.Names = names
		filteredBindings = append(filteredBindings, binding)
		for _, ref := range binding.Spec.ExtensionSets {
			selectedSets[ref.Name] = true
		}
		for _, ref := range binding.Spec.Extensions {
			selectedExtensions[ref.Name] = true
		}
	}
	setByName := map[string]v1alpha1.ClusterExtensionSet{}
	for _, set := range state.ClusterExtensionSets {
		setByName[set.Metadata.Name] = set
	}
	var visitSet func(string)
	visitSet = func(name string) {
		set, ok := setByName[name]
		if !ok {
			return
		}
		for _, ref := range set.Spec.ExtensionSets {
			if !selectedSets[ref.Name] {
				selectedSets[ref.Name] = true
				visitSet(ref.Name)
			}
		}
		for _, ref := range set.Spec.Extensions {
			selectedExtensions[ref.Name] = true
		}
	}
	for name := range selectedSets {
		visitSet(name)
	}
	var filteredSets []v1alpha1.ClusterExtensionSet
	for _, set := range state.ClusterExtensionSets {
		if selectedSets[set.Metadata.Name] {
			filteredSets = append(filteredSets, set)
		}
	}
	var filteredExtensions []v1alpha1.ClusterExtension
	for _, extension := range state.ClusterExtensions {
		if selectedExtensions[extension.Metadata.Name] {
			filteredExtensions = append(filteredExtensions, extension)
		}
	}
	state.ClusterExtensionBindings = filteredBindings
	state.ClusterExtensionSets = filteredSets
	state.ClusterExtensions = filteredExtensions
	return state
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
