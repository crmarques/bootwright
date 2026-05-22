package stategraph

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/stateview"
)

// DestroyScopeConflict describes one provider service component that is
// shared between a scoped cluster and an unscoped cluster.
type DestroyScopeConflict struct {
	Slot             string
	Provider         string
	Name             string
	ScopedClusters   []string
	UnscopedClusters []string
}

type sharedIdentity struct {
	slot, provider, name string
}

// SharedDestroyConflicts returns the provider service components that scoped
// clusters share with unscoped clusters. Destroying those services would break
// the unscoped consumers.
func SharedDestroyConflicts(state v1alpha1.State, selected []string) []DestroyScopeConflict {
	selectedSet := map[string]bool{}
	for _, n := range selected {
		selectedSet[n] = true
	}

	infraToClusters := map[string][]string{}
	for _, c := range state.ContainerClusters {
		for _, ref := range stateview.ClusterInfraNames(c) {
			infraToClusters[ref] = append(infraToClusters[ref], c.Metadata.Name)
		}
	}

	identityClusters := map[sharedIdentity]map[string]bool{}
	clusterByName := map[string]v1alpha1.ContainerCluster{}
	for _, cluster := range state.ContainerClusters {
		clusterByName[cluster.Metadata.Name] = cluster
	}
	for _, infra := range state.ClusterInfras {
		clusters := infraToClusters[infra.Metadata.Name]
		if len(clusters) == 0 {
			continue
		}
		for _, clusterName := range clusters {
			for _, id := range infraSharedIdentities(state, infra, clusterByName[clusterName]) {
				if identityClusters[id] == nil {
					identityClusters[id] = map[string]bool{}
				}
				identityClusters[id][clusterName] = true
			}
		}
	}

	var conflicts []DestroyScopeConflict
	for id, users := range identityClusters {
		var scoped, unscoped []string
		for user := range users {
			if selectedSet[user] {
				scoped = append(scoped, user)
			} else {
				unscoped = append(unscoped, user)
			}
		}
		if len(scoped) == 0 || len(unscoped) == 0 {
			continue
		}
		sort.Strings(scoped)
		sort.Strings(unscoped)
		conflicts = append(conflicts, DestroyScopeConflict{
			Slot:             id.slot,
			Provider:         id.provider,
			Name:             id.name,
			ScopedClusters:   scoped,
			UnscopedClusters: unscoped,
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Slot != conflicts[j].Slot {
			return conflicts[i].Slot < conflicts[j].Slot
		}
		if conflicts[i].Provider != conflicts[j].Provider {
			return conflicts[i].Provider < conflicts[j].Provider
		}
		return conflicts[i].Name < conflicts[j].Name
	})
	return conflicts
}

func infraSharedIdentities(state v1alpha1.State, infra v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) []sharedIdentity {
	var out []sharedIdentity
	add := func(slot, provider, name string) {
		if provider == "" {
			return
		}
		out = append(out, sharedIdentity{slot: slot, provider: provider, name: name})
	}
	c := infra.Spec.Components
	for _, lb := range c.LoadBalancers {
		add("loadBalancer", lb.From.Provider, lb.Name)
	}
	if c.Proxy != nil {
		add("proxy", c.Proxy.From.Provider, c.Proxy.From.Name)
	}
	if c.Registry != nil {
		add("registry", c.Registry.From.Provider, c.Registry.From.Name)
	}
	if artifactpub.ClusterNeedsPublication(state, infra, ocp) {
		if publisher, ok := artifactpub.Select(state); ok {
			add("artifacts", publisher.ProviderName, publisher.Capability.Name)
		}
	}
	if c.NameResolution != nil {
		add("nameResolution", c.NameResolution.From.Provider, c.NameResolution.From.Name)
	}
	return out
}

// FilterStateToClusters keeps selected ContainerClusters, their referenced
// ClusterInfras, and the InfraProviders needed by those ClusterInfras.
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
			if publisher, ok := artifactpub.Select(state); ok {
				selectedProviders[publisher.ProviderName] = true
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
