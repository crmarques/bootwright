package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

func prepareScopedWorkflow(scopedState v1alpha1.State, scope converge.Scope, askBecomePass, dryRun bool, records []ownership.ResourceRecord) (converge.WorkflowPlan, error) {
	return converge.PrepareScopedWorkflowPlan(scopedState, scope, scope.Phases(), askBecomePass, dryRun, records)
}

func prepareScopedApplyWorkflow(scopedState v1alpha1.State, scope converge.Scope, askBecomePass, dryRun bool) (converge.WorkflowPlan, error) {
	return converge.PrepareScopedWorkflowPlan(scopedState, scope, scope.ApplyPhases(), askBecomePass, dryRun, nil)
}
