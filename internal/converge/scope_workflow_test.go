package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPrepareScopedWorkflowPlanAddonsNoBindingsIsNoRemoteWork(t *testing.T) {
	scope := subPhaseStageScope("add-ons")
	plan, err := PrepareScopedWorkflowPlan(v1alpha1.State{}, scope, scope.ApplyPhases(), false, false, nil)
	if err != nil {
		t.Fatalf("PrepareScopedWorkflowPlan: %v", err)
	}
	if !plan.NoRemoteWork {
		t.Fatalf("addons scope with no bindings must be NoRemoteWork; got %+v", plan)
	}
}
