package status

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

// StateCheck classifies drift between the selected desired state and the last
// recorded apply, including Bootwright-owned resources that are no longer
// declared.
func StateCheck(state v1alpha1.State, clusterScope string, applyTarget workflow.ApplyTarget, runsDir, ownershipDir, contextName string) (workflow.StateCheckReport, error) {
	// Orphan detection compares the ownership records against the FULL declared
	// desired state, before --clusters scoping narrows it (otherwise a scoped check
	// would report other clusters' resources as undeclared).
	fullState := state
	// Resolve through the shared selector so the scoped state matches scoped
	// apply (same name validation, same ScopeStateForApply render set).
	sel, err := clusteraccess.Resolve(state, "all", clusterScope)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	state = sel.RenderState
	if sel.Active {
		// Mirror scoped apply exactly: provision-set (and so check-set) storage is
		// the directly-named storage roots only, never a data-foundation
		// render-reference pull-in (ADR-0004). StorageWorkNames is the single source
		// scoped apply uses for applyTarget.StorageClusterNames; reuse it so both
		// verbs plan the identical storage set.
		applyTarget.StorageClusterNames = sel.StorageWorkNames()
	}
	tasks, err := workflow.PlanApplyTasksChecked(applyTarget, state)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	report, err := workflow.StateCheck(tasks, applyTarget, state, runsDir)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	// Best-effort: report Bootwright-owned resources that are no longer declared
	// (orphans). Read-only — a failure to read records must not break the check.
	// Goes through the shared context-scoped loader so the record set matches what
	// destroy plans and executes over.
	if records, lerr := ownership.LoadContext(ownershipDir, contextName); lerr == nil {
		report.Undeclared = workflow.OwnershipOrphans(fullState, records)
	}
	return report, nil
}
