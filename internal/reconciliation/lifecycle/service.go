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
type CurrentSelection func(context.Context) (string, string, error)

type Executable struct{ Version, Commit string }

type Options struct {
	Confirmer  Confirmer
	Presenter  PlanPresenter
	Progress   ProgressReporter
	Clock      Clock
	Entropy    reconciliation.Entropy
	Selection  CurrentSelection
	Executable Executable
	Operations OperationStoreFactory
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
func (s Service) resolve(ctx context.Context, name string) (string, string, error) {
	if name != "" {
		return name, "", nil
	}
	if s.options.Selection == nil {
		return "", "", failure("context.state", "current context selection is not configured", "")
	}
	selected, id, err := s.options.Selection(ctx)
	if err != nil {
		return "", "", err
	}
	if selected == "" {
		return "", "", failure("context.state", "no current context is selected", "select one with context use --name <name>")
	}
	return selected, id, nil
}

func (s Service) store(view View) OperationStore {
	return s.options.Operations(view.Operations())
}

func (s Service) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	name, id, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var result *PlanResult
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		if err := verifySelection(view, id); err != nil {
			return err
		}
		preview, err := s.preview(ctx, view)
		if err != nil {
			return err
		}
		result = preview
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// preview derives the next legal operation or the exact continuation point of
// an incomplete one. It allocates no identity and writes nothing.
func (s Service) preview(ctx context.Context, view View) (*PlanResult, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return nil, err
	}
	result := &PlanResult{Context: view.Identity()}
	if index.Current == "" {
		plan, _, err := s.freshPlan(ctx, view, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		result.Verb, result.Steps = string(reconciliation.Apply), steps(plan, nil)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"}
		return result, nil
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
	if operation.Verb == reconciliation.Apply && operation.State == reconciliation.OperationDone {
		plan, _, err := s.freshPlan(ctx, view, reconciliation.Destroy)
		if err != nil {
			return nil, err
		}
		result.Verb, result.Steps = string(reconciliation.Destroy), steps(plan, nil)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "destroy"}
		return result, nil
	}
	if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationDone {
		plan, _, err := s.freshPlan(ctx, view, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		result.Verb, result.Steps = string(reconciliation.Apply), steps(plan, nil)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"}
		return result, nil
	}
	result.Verb, result.Steps, result.Continuation = string(operation.Verb), steps(frozen, states), true
	result.Receipt = Receipt{Operation: operation.ID, Verb: "plan", State: "preview", Next: continuationAction(operation, states)}
	return result, nil
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
	out := make([]PlanStep, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := string(reconciliation.BlockPending)
		if states != nil {
			if current, ok := states[block.ID]; ok && current != "" {
				state = string(current)
			}
		}
		out = append(out, PlanStep{ID: block.ID, Description: block.Description, Impacts: slices.Clone(block.Impacts), State: state})
	}
	return out
}

func verifySelection(view View, id string) error {
	if id != "" && view.Identity().ID != id {
		return failure("context.state", "the current context selection is stale", "select it again with context use --name "+view.Identity().Name)
	}
	return nil
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
