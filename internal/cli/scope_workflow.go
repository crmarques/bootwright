package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/runtime/ownership"
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

// prepareScopedWorkflow derives a destroy plan. Destroy playbooks tear down
// resources recorded in the context ownership store even when the desired state
// no longer references them, so callers must pass those records: the no-remote-work
// short-circuit (which suppresses the confirmation prompt and --yes gate) must see
// the same hosts workflow.Run does, or a recorded-but-undeclared teardown would run
// unattended.
func prepareScopedWorkflow(state v1alpha1.State, scope scopeSpec, clusterScope string, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (scopedWorkflowPlan, error) {
	return prepareScopedWorkflowWithPhases(state, scope, scope.phases(), clusterScope, askBecomePass, dryRun, records)
}

func prepareScopedApplyWorkflow(state v1alpha1.State, scope scopeSpec, clusterScope string, askBecomePass, dryRun bool) (scopedWorkflowPlan, error) {
	// Apply playbooks never consult ownership records (workflow.Run only loads
	// them for destroy playbooks), so the apply host count is desired-state only.
	return prepareScopedWorkflowWithPhasesAndStateFilter(state, scope, scope.applyPhases(), clusterScope, askBecomePass, dryRun, scopeStateForApply, nil)
}

func prepareScopedWorkflowWithPhases(state v1alpha1.State, scope scopeSpec, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (scopedWorkflowPlan, error) {
	return prepareScopedWorkflowWithPhasesAndStateFilter(state, scope, phaseList, clusterScope, askBecomePass, dryRun, scopeState, records)
}

func prepareScopedWorkflowWithPhasesAndStateFilter(state v1alpha1.State, scope scopeSpec, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool, filter func(v1alpha1.State, string, string) (v1alpha1.State, error), records []ownership.ResourceRecord) (scopedWorkflowPlan, error) {
	scopedState, err := filter(state, scope.name, clusterScope)
	if err != nil {
		return scopedWorkflowPlan{}, err
	}
	selected := phasesForState(phaseList, scopedState)
	limit := ansibleLimitForScope(scope.name)
	ansibleNoHosts := !dryRun && workflow.LimitMatchesNoHostsWithOwnershipRecords(limit, scopedState, records)
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
