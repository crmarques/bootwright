package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/ownership"
)

const (
	infraDestroyArtifactServerScope             = "artifact-server"
	infraDestroyArtifactServerPlaybook          = "bootwright.core.workflow_infra_destroy_artifact_server"
	infraDestroyArtifactServerArtifactsBaseName = "infra-destroy-artifact-server"
	infraComponentServiceScopeExtraVarName      = "bootwright_infra_component_service_scope"
)

var infraArtifactServerDestroyPhase = Phase{
	Name:        infraDestroyArtifactServerScope,
	NeedsRoot:   true,
	Description: "remove generated artifact publication service used for BMC ISO fetches",
}

func isInfraArtifactServerDestroyScope(scope scopeSpec, clusterScope string) bool {
	return scope.name == "infra" && strings.TrimSpace(clusterScope) == infraDestroyArtifactServerScope
}

func prepareInfraArtifactServerDestroyWorkflow(state v1alpha1.State, askBecomePass, dryRun bool, records []ownership.ResourceRecord) scopedWorkflowPlan {
	selected := []Phase{infraArtifactServerDestroyPhase}
	limit := render.GroupInfraComponentHosts
	noRemoteWork := !dryRun && workflow.LimitMatchesNoHostsWithOwnershipRecords(limit, state, records)
	askBecomeForRun := askBecomePass && rootPhaseCount(selected) > 0 && !noRemoteWork
	return scopedWorkflowPlan{
		state:         state,
		selected:      selected,
		limit:         limit,
		noRemoteWork:  noRemoteWork,
		askBecomePass: askBecomeForRun,
		extraVarPairs: []string{infraComponentServiceScopeExtraVarName + "=" + infraDestroyArtifactServerScope},
	}
}
