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
	return prepareScopedWorkflowWithPhasesAndStateFilter(state, scope, scope.applyPhases(), clusterScope, askBecomePass, dryRun, scopeStateForApply)
}

func prepareScopedWorkflowWithPhases(state v1alpha1.State, scope scopeSpec, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool) (scopedWorkflowPlan, error) {
	return prepareScopedWorkflowWithPhasesAndStateFilter(state, scope, phaseList, clusterScope, askBecomePass, dryRun, scopeState)
}

func prepareScopedWorkflowWithPhasesAndStateFilter(state v1alpha1.State, scope scopeSpec, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool, filter func(v1alpha1.State, string, string) (v1alpha1.State, error)) (scopedWorkflowPlan, error) {
	scopedState, err := filter(state, scope.name, clusterScope)
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
	if len(state.ClusterAddonBindings) == 0 {
		return false
	}
	for _, phase := range selected {
		if phase.Name == "addons" {
			return true
		}
	}
	return false
}
