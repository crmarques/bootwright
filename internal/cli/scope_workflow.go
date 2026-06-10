package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

// prepareScopedWorkflow derives a destroy plan. Destroy playbooks tear down
// resources recorded in the context ownership store even when the desired state
// no longer references them, so callers must pass those records: the no-remote-work
// short-circuit (which suppresses the confirmation prompt and --yes gate) must see
// the same hosts workflow.Run does, or a recorded-but-undeclared teardown would run
// unattended.
func prepareScopedWorkflow(state v1alpha1.State, scope converge.Scope, clusterScope string, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (converge.WorkflowPlan, error) {
	return prepareScopedWorkflowWithPhases(state, scope, scope.Phases(), clusterScope, askBecomePass, dryRun, records)
}

func prepareScopedApplyWorkflow(state v1alpha1.State, scope converge.Scope, clusterScope string, askBecomePass, dryRun bool) (converge.WorkflowPlan, error) {
	// Apply playbooks never consult ownership records (workflow.Run only loads
	// them for destroy playbooks), so the apply host count is desired-state only.
	return converge.PrepareScopedWorkflowPlan(state, scope, scope.ApplyPhases(), clusterScope, askBecomePass, dryRun, clusteraccess.ScopeStateForApply, nil)
}

func prepareScopedWorkflowWithPhases(state v1alpha1.State, scope converge.Scope, phaseList []converge.Phase, clusterScope string, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (converge.WorkflowPlan, error) {
	return converge.PrepareScopedWorkflowPlan(state, scope, phaseList, clusterScope, askBecomePass, dryRun, clusteraccess.ScopeState, records)
}
