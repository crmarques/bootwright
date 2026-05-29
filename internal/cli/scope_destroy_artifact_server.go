package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

const (
	infraDestroyArtifactServerScope             = "artifact-server"
	infraDestroyArtifactServerPlaybook          = "playbooks/targets/infra/destroy-artifact-server.yml"
	infraDestroyArtifactServerArtifactsBaseName = "infra-destroy-artifact-server"
	providerServiceScopeExtraVarName            = "bootwright_provider_service_scope"
)

var infraArtifactServerDestroyPhase = Phase{
	Name:        infraDestroyArtifactServerScope,
	NeedsRoot:   true,
	Description: "remove generated artifact publication service used for BMC ISO fetches",
}

func isInfraArtifactServerDestroyScope(scope scopeSpec, clusterScope string) bool {
	return scope.name == "infra" && strings.TrimSpace(clusterScope) == infraDestroyArtifactServerScope
}

func prepareInfraArtifactServerDestroyWorkflow(state v1alpha1.State, askBecomePass, dryRun bool) scopedWorkflowPlan {
	selected := []Phase{infraArtifactServerDestroyPhase}
	limit := render.GroupProviderHosts
	noRemoteWork := !dryRun && workflow.LimitMatchesNoHosts(limit, state)
	askBecomeForRun := askBecomePass && rootPhaseCount(selected) > 0 && !noRemoteWork
	return scopedWorkflowPlan{
		state:         state,
		selected:      selected,
		limit:         limit,
		noRemoteWork:  noRemoteWork,
		askBecomePass: askBecomeForRun,
		extraVarPairs: []string{providerServiceScopeExtraVarName + "=" + infraDestroyArtifactServerScope},
	}
}
