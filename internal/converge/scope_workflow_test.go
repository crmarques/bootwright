package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
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

func TestPrepareScopedWorkflowPlanCountsControllerOwnershipAsInfraWork(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: string(ownership.KindControllerNameResolver),
		Name: "resolver-record",
	}}
	plan, err := PrepareScopedWorkflowPlan(v1alpha1.State{}, InfraScope, InfraScope.Phases(), false, false, records)
	if err != nil {
		t.Fatalf("PrepareScopedWorkflowPlan: %v", err)
	}
	if plan.NoRemoteWork {
		t.Fatalf("controller-only ownership cleanup was classified as no work: %+v", plan)
	}
	if !strings.Contains(plan.Limit, render.GroupControllerHosts) {
		t.Fatalf("infra limit %q omits %q", plan.Limit, render.GroupControllerHosts)
	}
}
