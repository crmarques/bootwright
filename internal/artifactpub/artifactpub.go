package artifactpub

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

type Publisher struct {
	ProviderName string
	Capability   v1alpha1.ArtifactPublisherCapability
}

func All(state v1alpha1.State) []Publisher {
	var out []Publisher
	for _, provider := range state.InfraProviders {
		for _, publisher := range provider.Spec.ArtifactPublishers {
			out = append(out, Publisher{
				ProviderName: provider.Metadata.Name,
				Capability:   publisher,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		return out[i].Capability.Name < out[j].Capability.Name
	})
	return out
}

func Select(state v1alpha1.State) (Publisher, bool) {
	publishers := All(state)
	if len(publishers) != 1 {
		return Publisher{}, false
	}
	return publishers[0], true
}

func Names(publishers []Publisher) []string {
	out := make([]string, 0, len(publishers))
	for _, publisher := range publishers {
		out = append(out, publisher.ProviderName+"/"+publisher.Capability.Name)
	}
	sort.Strings(out)
	return out
}

func ClusterNeedsPublication(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) bool {
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		return true
	}
	return ClusterUsesBareMetalMachine(state, ci)
}

func ClusterUsesBareMetalMachine(state v1alpha1.State, ci v1alpha1.ClusterInfra) bool {
	for _, machine := range ci.Spec.Components.Machines {
		if machine.From.Name == "" {
			continue
		}
		provider, ok := stateview.Provider(state, machine.From.Provider)
		if !ok {
			continue
		}
		server, ok := stateview.Machine(provider, machine.From.Name)
		if !ok || v1alpha1.MachineProvisionerKind(server) != v1alpha1.ProvisionerBareMetal {
			continue
		}
		return true
	}
	return false
}
