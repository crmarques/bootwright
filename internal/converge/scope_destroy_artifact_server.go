package converge

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

const (
	infraDestroyArtifactServerScope             = "artifact-server"
	InfraDestroyArtifactServerPlaybook          = roles.PlaybookWorkflowInfraDestroyArtifactServer
	InfraDestroyArtifactServerArtifactsBaseName = "infra-destroy-artifact-server"
	InfraComponentServiceScopeExtraVarName      = "bootwright_infra_component_service_scope"
)

var infraArtifactServerDestroyPhase = Phase{
	Name:        infraDestroyArtifactServerScope,
	NeedsRoot:   true,
	Description: "remove generated artifact publication service used for BMC ISO fetches",
}

func IsInfraArtifactServerDestroyScope(scope Scope, clusterScope string) bool {
	return scope.Name == "infra" && strings.TrimSpace(clusterScope) == infraDestroyArtifactServerScope
}

func PrepareInfraArtifactServerDestroyWorkflow(state v1alpha1.State, askBecomePass, dryRun bool, records []ownership.ResourceRecord) WorkflowPlan {
	selected := []Phase{infraArtifactServerDestroyPhase}
	limit := render.GroupInfraComponentHosts
	noRemoteWork := !dryRun && workflow.LimitMatchesNoHostsWithOwnershipRecords(limit, state, records)
	askBecomeForRun := askBecomePass && RootPhaseCount(selected) > 0 && !noRemoteWork
	return WorkflowPlan{
		State:         state,
		Selected:      selected,
		Limit:         limit,
		NoRemoteWork:  noRemoteWork,
		AskBecomePass: askBecomeForRun,
		ExtraVarPairs: []string{InfraComponentServiceScopeExtraVarName + "=" + infraDestroyArtifactServerScope},
	}
}
