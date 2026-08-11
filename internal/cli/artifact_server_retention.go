package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
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

func reclaimApplyArtifactServers(runContext context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir, becomePasswordFile string, state v1alpha1.State, targets []converge.ArtifactServerReclaimTarget, executionExtraVarPairs []string, reporter *workflowReporter, runLease *workflow.CommandRunLease, enabled bool) {
	if !enabled {
		return
	}
	if err := converge.ReclaimInstallOnlyArtifactServers(runContext, stdout, stderr, ctx, clustersDir, executable, bundleDir, becomePasswordFile, state, targets, executionExtraVarPairs, reporter, runLease); err != nil {
		cliout.NewContinuation(stdout).Warning("artifact-server reclaim", err.Error())
	}
}
