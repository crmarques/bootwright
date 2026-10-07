package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
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
		return "", failure("context.state", "no current context is selected",
			"select one with bootwright context use --name <context>, or create one with bootwright context init --name <context>")
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
	var decided transition
	var read basis
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		previewed, from, durable, err := s.preview(ctx, view, selection)
		if err != nil {
			return err
		}
		result, decided, read = previewed, from, durable
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refuseLostBinding(ctx, name, decided, read); err != nil {
		return nil, err
	}
	return result, nil
}

// preview takes the decision of the verb it previews, through the path that
// verb takes, so it refuses exactly where that verb refuses and otherwise shows
// the plan the verb then presents. Where a finalization is due it decides as
// the verb does once that finalization is done, without performing it. It
// stops at the decision: it allocates no identity and writes nothing. It also
// returns that decision and the durable state it was read from, before any
// finalization it previews.
func (s Service) preview(ctx context.Context, view View, selection reconciliation.StageSelection) (*PlanResult, transition, basis, error) {
	verb, err := s.previewed(ctx, view)
	if err != nil {
		return nil, transition{}, basis{}, err
	}
	// A destroy accepts no stage selection, so its decision takes none, as
	// Destroy passes none.
	decideSelection := selection
	if verb == reconciliation.Destroy {
		decideSelection = nil
	}
	decided, err := s.decide(ctx, view, verb, decideSelection)
	if err != nil {
		return nil, transition{}, basis{}, err
	}
	marked, finalizes := decided, decided.finalize
	if finalizes {
		if decided, err = s.afterFinalization(ctx, view, verb, decideSelection, decided); err != nil {
			return nil, transition{}, basis{}, err
		}
	}
	if verb == reconciliation.Destroy && len(selection) != 0 {
		return nil, transition{}, basis{}, failure("lifecycle.stage",
			"the next operation is a destroy, which accepts no stage selection",
			"repeat "+contextCommand(view.Identity().Name, "plan")+" without --stage")
	}
	var result PlanResult
	switch {
	case decided.noop:
		// Only a finalization leads here, because no verb a preview decides as
		// settles over a record whose finalization is not due. What it calls
		// for is read from the record it finalizes, not the one it leaves.
		result = planPreview(decided.plan, decided.states, nil)
		result.Verb, result.Continuation, result.Finalizes = string(verb), true, true
		result.Receipt = Receipt{Operation: decided.operation.ID, Verb: "plan", State: "preview", Next: nextAction(marked.operation, marked.plan, marked.states)}
	case decided.fresh:
		result = presentation(decided)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: string(decided.verb)}
	default:
		result = presentation(decided)
		result.Receipt = Receipt{Operation: decided.operation.ID, Verb: "plan", State: "preview", Next: nextAction(decided.operation, decided.plan, decided.states)}
	}
	result.Context, result.Bound = view.Identity(), s.Concurrency()
	return &result, decided, marked.basis, nil
}

// previewed is the verb a preview decides as: a fresh apply over no operation
// or a completed destroy, the destroy of a completed apply, and an incomplete
// operation's own verb.
func (s Service) previewed(ctx context.Context, view View) (reconciliation.Verb, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return "", err
	}
	if index.Current == "" {
		return reconciliation.Apply, nil
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return "", err
	}
	switch {
	case operation.State != reconciliation.OperationDone:
		return operation.Verb, nil
	case operation.Verb == reconciliation.Apply:
		return reconciliation.Destroy, nil
	}
	return reconciliation.Apply, nil
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
			After: after, Wave: schedule.Waves[block.ID] + 1, Consumes: slices.Clone(block.Consumes),
		})
	}
	return out
}

// planPreview marks what a stage selection would start and why it would leave
// the rest, so an operator reads the consequence of the selection before
// confirming it. The marks are what the next apply's admission takes first:
// an unproved block it resolves or the failed block it retries holds back every
// other ready block, which is deferred behind the first of them. Without a
// selection the steps carry no marker.
func planPreview(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.StageSelection) PlanResult {
	schedule := reconciliation.ScheduleOf(plan)
	result := PlanResult{
		Steps: steps(plan, states), Stages: selection.Names(),
		Waves: schedule.Count, Widest: schedule.Widest,
	}
	if len(selection) == 0 {
		return result
	}
	kind, admitted := firstAdmission(plan, states, selection, nil)
	holding := ""
	if kind != StepStart {
		holding = heldBehind(plan, states, admitted)
	}
	deferrals := reconciliation.Deferrals(plan, states, selection)
	for index, step := range result.Steps {
		if slices.Contains(admitted, index) {
			result.Steps[index].Selection = kind
			result.Startable++
			continue
		}
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
		case holding != "":
			result.Steps[index].Selection, result.Steps[index].WaitsOn = StepWaiting, holding
			result.Deferred++
		}
	}
	return result
}

// heldBehind names the block a ready block waits on while admission resolves
// or retries: the first block it admits, or, where a selection admits no
// failed block, the first failed block, which the apply refuses over.
func heldBehind(plan reconciliation.Plan, states map[string]reconciliation.BlockState, admitted []int) string {
	if len(admitted) != 0 {
		return plan.Blocks[admitted[0]].ID
	}
	for _, block := range plan.Blocks {
		if states[block.ID] == reconciliation.BlockFailed {
			return block.ID
		}
	}
	return ""
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
