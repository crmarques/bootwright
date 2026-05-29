package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

const (
	infraDestroyHTTPServerScope             = "http-server"
	infraDestroyHTTPServerPlaybook          = "playbooks/targets/infra/destroy-http-server.yml"
	infraDestroyHTTPServerArtifactsBaseName = "infra-destroy-http-server"
	providerServiceScopeExtraVarName        = "bootwright_provider_service_scope"
)

var infraHTTPServerDestroyPhase = Phase{
	Name:        infraDestroyHTTPServerScope,
	NeedsRoot:   true,
	Description: "remove generated artifact HTTP service used for BMC ISO fetches",
}

func isInfraHTTPServerDestroyScope(scope scopeSpec, clusterScope string) bool {
	return scope.name == "infra" && strings.TrimSpace(clusterScope) == infraDestroyHTTPServerScope
}

func prepareInfraHTTPServerDestroyWorkflow(state v1alpha1.State, askBecomePass, dryRun bool) scopedWorkflowPlan {
	selected := []Phase{infraHTTPServerDestroyPhase}
	limit := render.GroupProviderHosts
	noRemoteWork := !dryRun && workflow.LimitMatchesNoHosts(limit, state)
	askBecomeForRun := askBecomePass && rootPhaseCount(selected) > 0 && !noRemoteWork
	return scopedWorkflowPlan{
		state:         state,
		selected:      selected,
		limit:         limit,
		noRemoteWork:  noRemoteWork,
		askBecomePass: askBecomeForRun,
		extraVarPairs: []string{providerServiceScopeExtraVarName + "=" + infraDestroyHTTPServerScope},
	}
}
