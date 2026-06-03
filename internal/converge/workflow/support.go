package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func EnsureApplySupported(state v1alpha1.State) error {
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Management == v1alpha1.StorageClusterManagementFullManaged {
			return fmt.Errorf("StorageCluster/%s spec.management=fullManaged is recognized but fullManaged storage infra is not available yet", cluster.Metadata.Name)
		}
	}
	known := map[string]bool{}
	for _, p := range state.InfraProviders {
		known[p.Metadata.Name] = true
	}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Nodes {
			if m.Source.ProviderRef.Name == "" {
				continue
			}
			if !known[m.Source.ProviderRef.Name] {
				return fmt.Errorf("ClusterInfra/%s node %s references unknown provider %q", ci.Metadata.Name, m.Name, m.Source.ProviderRef.Name)
			}
			dispatchSupport := render.ProviderDriver(state, m)
			if !dispatchSupport.ApplySupported() {
				dispatch := dispatchSupport.Dispatch
				return fmt.Errorf("ClusterInfra/%s node %s resolves to unsupported apply dispatch substrate=%s bmc=%s boot=%s: %s", ci.Metadata.Name, m.Name, dispatch.SubstrateRole, dispatch.BMCRole, dispatch.BootRole, dispatchSupport.Summary)
			}
		}
	}
	return nil
}
