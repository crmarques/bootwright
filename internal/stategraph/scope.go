package stategraph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/stateview"
)

// FilterStateToClusters keeps selected ContainerClusters, their referenced
// ClusterInfras, and the providers/components needed by those ClusterInfras.
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
	selectedComponents := map[string]bool{}
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
		for _, c := range infra.Spec.Components.LoadBalancers {
			if c.From.Provider != "" {
				selectedProviders[c.From.Provider] = true
			}
		}
		for _, c := range []*v1alpha1.ClusterComponentRef{
			infra.Spec.Components.Proxy,
			infra.Spec.Components.Registry,
		} {
			if c != nil && c.From.Provider != "" {
				selectedProviders[c.From.Provider] = true
			}
		}
		if ocp, ok := SelectedClusterForInfra(filteredOCP, infra.Metadata.Name); ok && artifactpub.ClusterNeedsPublication(state, infra, ocp) {
			if server, ok := artifactpub.Select(state); ok {
				selectedComponents[server.Component.Metadata.Name] = true
			}
		}
		if infra.Spec.Components.NameResolution != nil && infra.Spec.Components.NameResolution.From.Provider != "" {
			selectedProviders[infra.Spec.Components.NameResolution.From.Provider] = true
		}
	}

	filteredProviders := make([]v1alpha1.InfraProvider, 0, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		if selectedProviders[provider.Metadata.Name] {
			filteredProviders = append(filteredProviders, provider)
		}
	}

	state.InfraProviders = filteredProviders
	filteredComponents := make([]v1alpha1.InfraComponent, 0, len(state.InfraComponents))
	for _, component := range state.InfraComponents {
		if selectedComponents[component.Metadata.Name] {
			filteredComponents = append(filteredComponents, component)
		}
	}
	state.InfraComponents = filteredComponents
	state.ClusterInfras = filteredInfra
	state.ContainerClusters = filteredOCP
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
