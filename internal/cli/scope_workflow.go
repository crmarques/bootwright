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
	return prepareScopedWorkflowWithPhases(state, scope, scope.phases(), clusterScope, askBecomePass, dryRun)
}

func prepareScopedApplyWorkflow(state v1alpha1.State, scope scopeSpec, clusterScope string, askBecomePass, dryRun bool) (scopedWorkflowPlan, error) {
	return prepareScopedWorkflowWithPhases(state, scope, scope.applyPhases(), clusterScope, askBecomePass, dryRun)
}

func prepareScopedWorkflowWithPhases(state v1alpha1.State, scope scopeSpec, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool) (scopedWorkflowPlan, error) {
	scopedState, err := scopeState(state, scope.name, clusterScope)
	if err != nil {
		return scopedWorkflowPlan{}, err
	}
	selected := phasesForState(phaseList, scopedState)
	limit := ansibleLimitForScope(scope.name)
	ansibleNoHosts := !dryRun && workflow.LimitMatchesNoHosts(limit, scopedState)
	noRemoteWork := ansibleNoHosts && !selectedHasExtensionWork(selected, scopedState)
	askBecomeForRun := askBecomePass && rootPhaseCount(selected) > 0 && !ansibleNoHosts
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

func selectedHasExtensionWork(selected []Phase, state v1alpha1.State) bool {
	if len(state.ClusterExtensionBindings) == 0 {
		return false
	}
	for _, phase := range selected {
		if phase.Name == "extensions" {
			return true
		}
	}
	return false
}
