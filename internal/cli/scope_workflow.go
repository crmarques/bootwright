package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

// prepareScopedWorkflow derives a destroy plan from a state already narrowed to
// the run's cluster selection (clusteraccess.Selection.RenderState). Destroy
// playbooks tear down resources recorded in the context ownership store even
// when the desired state no longer references them, so callers must pass those
// records: the no-remote-work short-circuit (which suppresses the confirmation
// prompt and --yes gate) must see the same hosts workflow.Run does, or a
// recorded-but-undeclared teardown would run unattended.
func prepareScopedWorkflow(scopedState v1alpha1.State, scope converge.Scope, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (converge.WorkflowPlan, error) {
	return converge.PrepareScopedWorkflowPlan(scopedState, scope, scope.Phases(), askBecomePass, dryRun, records)
}

func prepareScopedApplyWorkflow(scopedState v1alpha1.State, scope converge.Scope, askBecomePass, dryRun bool) (converge.WorkflowPlan, error) {
	// Apply playbooks never consult ownership records (workflow.Run only loads
	// them for destroy playbooks), so the apply host count is desired-state only.
	return converge.PrepareScopedWorkflowPlan(scopedState, scope, scope.ApplyPhases(), askBecomePass, dryRun, nil)
}
