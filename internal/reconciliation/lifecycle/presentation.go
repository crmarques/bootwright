package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func (s Service) present(ctx context.Context, name string, decided operationPlan) error {
	if s.options.Presenter == nil {
		return failure("lifecycle.state", "lifecycle plan presentation is not configured", "")
	}
	result := s.presentation(decided)
	result.Context, result.Bound = ContextIdentity{Name: name}, s.Concurrency()
	result.Receipt = Receipt{Operation: "none", Verb: string(decided.verb), State: "preview", Next: string(decided.verb)}
	if !decided.fresh {
		result.Receipt.Operation = decided.operation.ID
		result.Receipt.Next = "continue-" + string(decided.verb)
	}
	return s.options.Presenter.PresentLifecyclePlan(ctx, result)
}

// presentation is the plan a transition shows: what apply and destroy present
// before they confirm, and what plan previews of the same transition.
func (s Service) presentation(decided operationPlan) PlanResult {
	result := planPreview(decided.plan, decided.states, decided.selection, s.Throttle())
	result.Verb, result.Warnings = string(decided.verb), s.removalWarnings(decided.removed())
	result.Continuation = !decided.fresh
	result.AddsTo = decided.predecessor
	if decided.verb == reconciliation.Destroy && len(decided.takes) > 1 {
		result.TakesBack = slices.Clone(decided.takes)
	}
	// Ordinary removal keeps its pre-registration gate. An online removal's
	// later Machine boundary is re-proved on each continuation that needs it.
	if decided.fresh && decided.verb == reconciliation.Destroy {
		result.Stops = s.stopsFirst(decided.plan, decided.phases)
	}
	if decided.verb == reconciliation.Destroy {
		result.StopsAfterOnline = stopsAfterOnline(decided.plan, decided.phases, decided.states)
	}
	if result.Continuation && !result.Finalizes && decided.operation.ID != "" && s.otherBuild(decided.operation) && decided.sameBlocks.proved {
		result.AnotherBuild = anotherBuildOf(decided.operation, result.Steps)
	}
	return result
}

func anotherBuildOf(operation operationstore.Operation, steps []PlanStep) *AnotherBuild {
	var running []int
	for index, step := range steps {
		if step.State != string(reconciliation.BlockDone) && step.Selection != StepNotSelected {
			running = append(running, index+1)
		}
	}
	if len(running) == 0 {
		return nil
	}
	return &AnotherBuild{RegisteredBy: buildIdentity(operation.Executable), Steps: running}
}
