package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func installOnlyArtifactServerTargets(state v1alpha1.State) []converge.ArtifactServerReclaimTarget {
	refByServer := map[string][]string{}
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.Identity.Kind != v1alpha1.ComponentSlotArtifactServer {
			continue
		}
		refByServer[service.Identity.Name] = service.ConsumerClusters()
	}
	var out []converge.ArtifactServerReclaimTarget
	for _, component := range state.InfraComponents {
		server := component.Spec.ArtifactServer
		if server == nil || server.RetentionMode() != v1alpha1.ArtifactServerRetentionInstallOnly {
			continue
		}
		out = append(out, converge.ArtifactServerReclaimTarget{
			RecordName:  converge.ArtifactServerRecordName(component.Metadata.Name),
			RefClusters: refByServer[component.Metadata.Name],
		})
	}
	return out
}
