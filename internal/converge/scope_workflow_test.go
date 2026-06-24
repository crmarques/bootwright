package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestPrepareScopedWorkflowPlanAddonsNoBindingsIsNoRemoteWork pins the addons
// short-circuit: the addons scope runs no ansible, so with no addon bindings
// there is nothing to do and the run must report NoRemoteWork. Before the
// AnsibleLimitForScope("addons")=="" empty-limit handling, this stayed false
// and an empty addons run still prompted and started.
func TestPrepareScopedWorkflowPlanAddonsNoBindingsIsNoRemoteWork(t *testing.T) {
	passthrough := func(s v1alpha1.State, _, _ string) (v1alpha1.State, error) { return s, nil }
	scope := subPhaseStageScope("addons")
	plan, err := PrepareScopedWorkflowPlan(v1alpha1.State{}, scope, scope.ApplyPhases(), "", false, false, passthrough, nil)
	if err != nil {
		t.Fatalf("PrepareScopedWorkflowPlan: %v", err)
	}
	if !plan.NoRemoteWork {
		t.Fatalf("addons scope with no bindings must be NoRemoteWork; got %+v", plan)
	}
}
