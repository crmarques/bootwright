package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// CurrentSelection resolves the invoking user's context when none is explicit.
// It returns the selected name and identity so a stale marker is caught.
type CurrentSelection func(context.Context) (string, error)

type Executable struct{ Version, Commit string }

type Options struct {
	Confirmer Confirmer
	Presenter PlanPresenter
	Progress  ProgressReporter
	Clock     Clock
	Entropy   reconciliation.Entropy
	Selection CurrentSelection
	// Concurrency bounds how many of an operation's blocks run at once. It is
	// how much this host is asked to do at the same time, never what the plan
	// permits, so it is not frozen with the plan. Zero takes the default.
	Concurrency int
	Executable  Executable
	Operations  OperationStoreFactory
}

type Service struct {
	workspace    Workspace
	inputs       Inputs
	compiler     Compiler
	binder       SecretBinder
	host         HostIdentity
	automation   AutomationIdentity
	guard        ExecutionGuard
	capabilities CapabilityResolver
	options      Options
}

func New(workspace Workspace, inputs Inputs, compiler Compiler, binder SecretBinder, host HostIdentity, automation AutomationIdentity, guard ExecutionGuard, capabilities CapabilityResolver, options Options) Service {
	return Service{workspace, inputs, compiler, binder, host, automation, guard, capabilities, options}
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.workspace == nil || s.compiler == nil || s.binder == nil || s.host == nil || s.automation == nil || s.guard == nil || s.capabilities == nil {
		return availability.ErrNotImplemented
	}
	if s.options.Clock == nil || s.options.Entropy == nil || s.options.Operations == nil {
		return availability.ErrNotImplemented
	}
	return nil
}

// resolve fixes the context this invocation acts on. An omitted name uses the
// invoking user's selection and refuses a stale marker.
func (s Service) resolve(ctx context.Context, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	if s.options.Selection == nil {
		return "", failure("context.state", "current context selection is not configured", "")
	}
	selected, err := s.options.Selection(ctx)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", failure("context.state", "no current context is selected", "select one with context use --name <name>")
	}
	return selected, nil
}

func (s Service) store(view View) OperationStore {
	return s.options.Operations(view.Operations())
}

func (s Service) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	selection, err := reconciliation.ParseStages(request.Stages)
	if err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var result *PlanResult
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		previewed, err := s.preview(ctx, view, selection)
		if err != nil {
			return err
		}
		result = previewed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// preview derives the next legal operation or the exact continuation point of
// an incomplete one. It allocates no identity and writes nothing.
func (s Service) preview(ctx context.Context, view View, selection reconciliation.StageSelection) (*PlanResult, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return nil, err
	}
	// A fresh preview is the fresh apply's own decision, so it refuses exactly
	// what that apply would refuse before it registers anything.
	fresh := func() (*PlanResult, error) {
		decided, err := s.freshApply(ctx, view, selection)
		if err != nil {
			return nil, err
		}
		result := planPreview(decided.plan, nil, selection)
		result.Context, result.Verb = view.Identity(), string(decided.verb)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: string(decided.verb)}
		return &result, nil
	}
	if index.Current == "" {
		return fresh()
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return nil, err
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return nil, err
	}
	states, err := store.BlockStates(ctx, operation.ID, frozen)
	if err != nil {
		return nil, err
	}
	// A removal is planned from the plan its apply froze, so the preview shows
	// the removal destroy would perform rather than what this executable would
	// derive from the same input today.
	if operation.Verb == reconciliation.Apply && operation.State == reconciliation.OperationDone {
		removal, err := s.removalOf(ctx, reconciliation.OwnedSubset(frozen, states), operation.Executable)
		if err != nil {
			return nil, err
		}
		result := planPreview(removal, nil, selection)
		result.Context, result.Verb = view.Identity(), string(reconciliation.Destroy)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "destroy"}
		return &result, nil
	}
	if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationDone {
		return fresh()
	}
	result := planPreview(frozen, states, selection)
	result.Context, result.Verb, result.Continuation = view.Identity(), string(operation.Verb), true
	result.Receipt = Receipt{Operation: operation.ID, Verb: "plan", State: "preview", Next: continuationAction(operation, states)}
	return &result, nil
}

func continuationAction(operation operationstore.Operation, states map[string]reconciliation.BlockState) string {
	for _, state := range states {
		if state == reconciliation.BlockUnknown {
			return "resolve"
		}
	}
	if operation.Verb == reconciliation.Destroy {
		return "continue-destroy"
	}
	return "continue-apply"
}

func steps(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []PlanStep {
	schedule := reconciliation.ScheduleOf(plan)
	position := make(map[string]int, len(plan.Blocks))
	for index, block := range plan.Blocks {
		position[block.ID] = index + 1
	}
	out := make([]PlanStep, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := string(reconciliation.BlockPending)
		if states != nil {
			if current, ok := states[block.ID]; ok && current != "" {
				state = string(current)
			}
		}
		var after []int
		for _, dependency := range block.Dependencies {
			if place, known := position[dependency]; known {
				after = append(after, place)
			}
		}
		slices.Sort(after)
		out = append(out, PlanStep{
			ID: block.ID, Description: block.Description, Stage: string(block.Stage),
			Impacts: slices.Clone(block.Impacts), State: state,
			After: after, Wave: schedule.Waves[block.ID] + 1,
		})
	}
	return out
}

// planPreview marks what a stage selection would start and why it would leave
// the rest, so an operator reads the consequence of the selection before
// confirming it. Without a selection the steps carry no marker.
func planPreview(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.StageSelection) PlanResult {
	schedule := reconciliation.ScheduleOf(plan)
	result := PlanResult{
		Steps: steps(plan, states), Stages: selection.Names(),
		Waves: schedule.Count, Widest: schedule.Widest,
	}
	if len(selection) == 0 {
		return result
	}
	deferrals := reconciliation.Deferrals(plan, states, selection)
	startable := reconciliation.Startable(plan, states, selection)
	for index, step := range result.Steps {
		if step.State != string(reconciliation.BlockPending) {
			continue
		}
		deferral, waiting := deferrals[step.ID]
		switch {
		case waiting && deferral.Reason == reconciliation.DeferredNotSelected:
			result.Steps[index].Selection = StepNotSelected
			result.Deferred++
		case waiting:
			result.Steps[index].Selection, result.Steps[index].WaitsOn = StepWaiting, deferral.Block
			result.Deferred++
		case slices.ContainsFunc(startable, func(block reconciliation.Block) bool { return block.ID == step.ID }):
			result.Steps[index].Selection = StepStart
			result.Startable++
		}
	}
	return result
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
