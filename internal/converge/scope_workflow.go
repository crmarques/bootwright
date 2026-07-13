package converge

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

type WorkflowPlan struct {
	State            v1alpha1.State
	Selected         []Phase
	Limit            string
	NoRemoteWork     bool
	AskBecomePass    bool
	ExtraVarPairs    []string
	TargetsClusters  bool
	StorageWorkNames []string
}

func PrepareScopedWorkflowPlan(scopedState v1alpha1.State, scope Scope, phaseList []Phase, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (WorkflowPlan, error) {
	selected := PhasesForState(phaseList, scopedState)
	limit := scope.AnsibleLimit
	ansibleNoHosts := !dryRun && (!ScopeUsesAnsible(scope) || workflow.LimitMatchesNoHostsWithOwnershipRecords(limit, scopedState, records))
	noRemoteWork := ansibleNoHosts && !selectedHasExtensionWork(selected, scopedState)
	askBecomeForRun := askBecomePass && RootPhaseCount(selected) > 0 && !ansibleNoHosts
	return WorkflowPlan{
		State:           scopedState,
		Selected:        selected,
		Limit:           limit,
		NoRemoteWork:    noRemoteWork,
		AskBecomePass:   askBecomeForRun,
		ExtraVarPairs:   nil,
		TargetsClusters: SelectedTargetsClusters(selected),
	}, nil
}

func selectedHasExtensionWork(selected []Phase, state v1alpha1.State) bool {
	addonsSelected := false
	for _, phase := range selected {
		if phase.Name == "add-ons" {
			addonsSelected = true
			break
		}
	}
	if !addonsSelected {
		return false
	}
	return len(state.ClusterAddonBindings) > 0 || len(state.ProvisioningPlaybooks) > 0
}
