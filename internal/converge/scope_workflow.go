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
	// StorageWorkNames restricts which StorageClusters a destroy actually tears
	// down (the directly-selected storage roots), mirroring how apply restricts
	// provisioning to applyTarget.StorageClusterNames. State stays
	// render-inclusive so a container cluster's data-foundation attachment still
	// renders, but a render-reference StorageCluster pulled in only by that
	// attachment is never a teardown target. nil means no --clusters narrowing
	// (tear down every StorageCluster in State); a non-nil empty slice means a
	// selection that names no storage cluster (tear down none).
	StorageWorkNames []string
}

// PrepareScopedWorkflowPlan builds the workflow plan from a state already
// narrowed to the run's scope and cluster selection. Cluster selection is a CLI
// concern resolved by internal/clusteraccess.Resolve; converge takes the
// pre-scoped State (and, for destroy, the storage work set via the caller) as
// plain inputs rather than a filter function.
func PrepareScopedWorkflowPlan(scopedState v1alpha1.State, scope Scope, phaseList []Phase, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (WorkflowPlan, error) {
	selected := PhasesForState(phaseList, scopedState)
	limit := scope.AnsibleLimit
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
		if phase.Name == "add-ons" {
			return true
		}
	}
	return false
}
