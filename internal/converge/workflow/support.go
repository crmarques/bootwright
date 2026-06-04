package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/view"
)

func EnsureApplySupported(state v1alpha1.State) error {
	known := map[string]bool{}
	for _, p := range state.InfraProviders {
		known[p.Metadata.Name] = true
	}
	for _, cluster := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, cluster)
		if !ok {
			continue
		}
		for _, m := range ci.Machines {
			if m.Source.ProviderRef.Name == "" {
				continue
			}
			if !known[m.Source.ProviderRef.Name] {
				return fmt.Errorf("ContainerCluster/%s node %s references unknown provider %q", cluster.Metadata.Name, m.Name, m.Source.ProviderRef.Name)
			}
			dispatchSupport := render.ProviderDriver(state, m)
			if !dispatchSupport.ApplySupported() {
				dispatch := dispatchSupport.Dispatch
				return fmt.Errorf("ContainerCluster/%s node %s resolves to unsupported apply dispatch substrate=%s bmc=%s boot=%s: %s", cluster.Metadata.Name, m.Name, dispatch.SubstrateRole, dispatch.BMCRole, dispatch.BootRole, dispatchSupport.Summary)
			}
		}
	}
	return nil
}
