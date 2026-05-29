package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type scopedWorkflowPlan struct {
	state           v1alpha1.State
	selected        []Phase
	limit           string
	noRemoteWork    bool
	askBecomePass   bool
	extraVarPairs   []string
	targetsClusters bool
}

func prepareScopedWorkflow(state v1alpha1.State, scope scopeSpec, clusterScope string, askBecomePass, dryRun bool) (scopedWorkflowPlan, error) {
	scopedState, err := scopeState(state, scope.name, clusterScope)
	if err != nil {
		return scopedWorkflowPlan{}, err
	}
	selected := phasesForState(scope.phases(), scopedState)
	limit := ansibleLimitForScope(scope.name)
	noRemoteWork := !dryRun && workflow.LimitMatchesNoHosts(limit, scopedState)
	askBecomeForRun := askBecomePass && rootPhaseCount(selected) > 0 && !noRemoteWork
	return scopedWorkflowPlan{
		state:           scopedState,
		selected:        selected,
		limit:           limit,
		noRemoteWork:    noRemoteWork,
		askBecomePass:   askBecomeForRun,
		extraVarPairs:   resolvedOCPBinaryPairs(selected),
		targetsClusters: selectedTargetsClusters(selected),
	}, nil
}
