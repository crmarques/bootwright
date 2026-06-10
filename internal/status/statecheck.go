package status

import (
	"strings"

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
	scoped := strings.TrimSpace(clusterScope) != ""
	if scoped {
		if _, _, err := clusteraccess.ClusterRootNamesForTarget(state, clusterScope); err != nil {
			return workflow.StateCheckReport{}, err
		}
	}
	state, err := clusteraccess.ScopeStateForApply(state, "all", clusterScope)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	if scoped {
		// Match scoped apply: report the transitive data-foundation
		// attachment-target storage clusters present in the scoped state, not
		// only the literal --clusters storage names.
		names := make([]string, 0, len(state.StorageClusters))
		for _, sc := range state.StorageClusters {
			names = append(names, sc.Metadata.Name)
		}
		applyTarget.StorageClusterNames = names
	}
	tasks, err := workflow.PlanApplyTasksChecked(applyTarget, state)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	report, err := workflow.StateCheck(tasks, runsDir)
	if err != nil {
		return workflow.StateCheckReport{}, err
	}
	// Best-effort: report Bootwright-owned resources that are no longer declared
	// (orphans). Read-only — a failure to read records must not break the check.
	if records, lerr := ownership.LoadResources(ownershipDir); lerr == nil {
		report.Undeclared = workflow.OwnershipOrphans(fullState, ownership.FilterByContext(records, contextName))
	}
	return report, nil
}
