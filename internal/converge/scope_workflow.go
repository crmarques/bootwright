package converge

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

type WorkflowPlan struct {
	State           v1alpha1.State
	Selected        []Phase
	Limit           string
	NoRemoteWork    bool
	AskBecomePass   bool
	ExtraVarPairs   []string
	TargetsClusters bool
}

// StateScopeFilter narrows the desired state to a scope target and cluster
// selection. The CLI binds it to internal/clusteraccess selection helpers
// (ScopeState / ScopeStateForApply); converge takes the function as a
// parameter so cluster selection stays a CLI concern.
type StateScopeFilter func(v1alpha1.State, string, string) (v1alpha1.State, error)

func PrepareScopedWorkflowPlan(state v1alpha1.State, scope Scope, phaseList []Phase, clusterScope string, askBecomePass, dryRun bool, filter StateScopeFilter, records []ownership.ResourceRecord) (WorkflowPlan, error) {
	scopedState, err := filter(state, scope.Name, clusterScope)
	if err != nil {
		return WorkflowPlan{}, err
	}
	selected := PhasesForState(phaseList, scopedState)
	limit := AnsibleLimitForScope(scope.Name)
	// A scope that runs no ansible (addons) has no ansible hosts by definition.
	// Its limit is empty, and LimitMatchesNoHosts returns false for an empty
	// limit, so without this it could never reach NoRemoteWork — an addons run
	// with zero bindings would still prompt and start on an empty plan.
	ansibleNoHosts := !dryRun && (!ScopeUsesAnsible(scope) || workflow.LimitMatchesNoHostsWithOwnershipRecords(limit, scopedState, records))
	noRemoteWork := ansibleNoHosts && !selectedHasExtensionWork(selected, scopedState)
	askBecomeForRun := askBecomePass && RootPhaseCount(selected) > 0 && !ansibleNoHosts
	return WorkflowPlan{
		State:           scopedState,
		Selected:        selected,
		Limit:           limit,
		NoRemoteWork:    noRemoteWork,
		AskBecomePass:   askBecomeForRun,
		ExtraVarPairs:   resolvedOCPBinaryPairs(selected),
		TargetsClusters: SelectedTargetsClusters(selected),
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
