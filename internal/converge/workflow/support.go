package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func EnsureApplySupported(state v1alpha1.State) error {
	known := map[string]bool{}
	for _, p := range state.InfraProviders {
		known[p.Metadata.Name] = true
	}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Machines {
			if !known[m.From.Provider] {
				return fmt.Errorf("ClusterInfra/%s machine %s references unknown provider %q", ci.Metadata.Name, m.Name, m.From.Provider)
			}
			dispatchSupport := render.ProviderDriver(state, m)
			if !dispatchSupport.ApplySupported() {
				dispatch := dispatchSupport.Dispatch
				return fmt.Errorf("ClusterInfra/%s machine %s resolves to unsupported apply dispatch substrate=%s bmc=%s boot=%s: %s", ci.Metadata.Name, m.Name, dispatch.SubstrateRole, dispatch.BMCRole, dispatch.BootRole, dispatchSupport.Summary)
			}
		}
	}
	return nil
}
