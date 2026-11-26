package lifecycle

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type PreparationDefinition struct {
	Automatic     func(context.Context, PlanInput, reconciliation.Selection) ([]string, error)
	Kind          string
	Command       string
	Bindings      []CapabilityBinding
	Prerequisites []CapabilityBinding
	Bearing       EffectBearing
	Validate      func(context.Context, PlanInput, reconciliation.PreparationScope) error
	Advance       func(context.Context, PreparationAdvance) error
}

type PreparationAdvance struct {
	AppliedEvidence func(context.Context, string, string) ([]BlockEvidence, error)
	Scope           reconciliation.PreparationScope
	Context         ContextIdentity
	State           *compilation.State
	Before          *compilation.State
	Owned           reconciliation.Plan
	Fresh           reconciliation.Plan
}

type PrepareRequest struct {
	ContextName          string
	Kind                 string
	Targets              []string
	PreparePrerequisites bool
	SkipConfirmation     bool
}

func validPreparationDefinition(d PreparationDefinition) bool {
	return d.Command != "" && d.Validate != nil && d.Advance != nil && d.Bearing != nil && len(d.Bindings) != 0
}

func (s Service) preparationDefinition(kind string) (PreparationDefinition, bool) {
	return preparationDefinitionOf(s.options.Preparations, kind)
}

func preparationDefinitionOf(definitions []PreparationDefinition, kind string) (PreparationDefinition, bool) {
	for _, definition := range definitions {
		if definition.Kind == kind {
			return definition, true
		}
	}
	return PreparationDefinition{}, false
}

func (s Service) Prepare(ctx context.Context, request PrepareRequest) (*OperationResult, error) {
	scope, err := s.requestedPreparation(request)
	if err != nil {
		return nil, err
	}
	s = s.withExplicitPreparation(scope, false)
	return s.applyWithin(ctx, ApplyRequest{ContextName: request.ContextName, SkipConfirmation: request.SkipConfirmation}, scope)
}

func (s Service) PlanPreparation(ctx context.Context, request PrepareRequest) (*PlanResult, error) {
	scope, err := s.requestedPreparation(request)
	if err != nil {
		return nil, err
	}
	return s.withExplicitPreparation(scope, true).planWithin(ctx, PlanRequest{Verb: reconciliation.Apply, ContextName: request.ContextName}, scope)
}

func (s Service) requestedPreparation(request PrepareRequest) (*reconciliation.PreparationScope, error) {
	definition, found := s.preparationDefinition(request.Kind)
	if !found {
		return nil, prepareUnsupported.Fail("the preparation owner is not registered", "")
	}
	targets := slices.Clone(request.Targets)
	slices.Sort(targets)
	return s.admitPreparation(reconciliation.PreparationScope{Kind: request.Kind, Command: definition.Command, Targets: slices.Compact(targets), PreparePrerequisites: request.PreparePrerequisites})
}

func (s Service) admitPreparation(scope reconciliation.PreparationScope) (*reconciliation.PreparationScope, error) {
	if err := scope.Validate(); err != nil {
		return nil, prepareRefused.Fail(err.Error(), "")
	}
	definition, found := s.preparationDefinition(scope.Kind)
	if !found || !validPreparationDefinition(definition) || !definition.admitsCommand(scope.Command) {
		return nil, prepareUnsupported.Fail("this executable has no registered preparation for "+scope.Kind, "")
	}
	return &scope, nil
}

func (s Service) planningBindings(scope *reconciliation.PreparationScope) []CapabilityBinding {
	if scope == nil {
		return s.capabilities.Bindings()
	}
	definition, _ := s.preparationDefinition(scope.Kind)
	bindings := slices.Clone(definition.Bindings)
	if scope.PreparePrerequisites {
		bindings = append(bindings, definition.Prerequisites...)
	}
	return bindings
}

type preparationResolver struct {
	CapabilityResolver
	bindings []CapabilityBinding
}

func (r preparationResolver) Bindings() []CapabilityBinding { return slices.Clone(r.bindings) }

func (s Service) preparationOwned(ctx context.Context, view View) (reconciliation.Plan, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil || index.Current == "" {
		return reconciliation.Plan{}, err
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	if operation.Verb != reconciliation.Apply {
		if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationDone {
			return reconciliation.Plan{}, nil
		}
		return reconciliation.Plan{}, prepareRefused.Fail("a preparation cannot replace a removal", "")
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: frozen})
	if err != nil {
		return reconciliation.Plan{}, err
	}
	return chain.plan()
}

func (s Service) retainPreparedOwnership(ctx context.Context, view View, definitions []reconciliation.BlockDefinition, scope *reconciliation.PreparationScope) ([]reconciliation.BlockDefinition, error) {
	if scope == nil {
		return definitions, nil
	}
	owned, err := s.preparationOwned(ctx, view)
	if err != nil {
		return nil, err
	}
	for _, block := range owned.Blocks {
		if !slices.ContainsFunc(definitions, func(d reconciliation.BlockDefinition) bool { return d.ID == block.ID }) {
			definition := block.BlockDefinition
			for _, binding := range s.capabilities.Bindings() {
				if binding.Kind == block.Kind && binding.Implementation == block.Implementation {
					definition.Family = binding.family()
					break
				}
			}
			definitions = append(definitions, definition)
		}
	}
	return definitions, nil
}

func (s Service) validatePreparationScope(ctx context.Context, input PlanInput) error {
	if input.Preparation == nil {
		return nil
	}
	definition, _ := s.preparationDefinition(input.Preparation.Kind)
	return definition.Validate(ctx, input, *input.Preparation)
}

func (d decider) admitPreparationCommand(frozen reconciliation.Plan, operation operationstore.Operation) error {
	if operation.Verb != reconciliation.Apply || operation.State == reconciliation.OperationDone {
		return nil
	}
	if reflect.DeepEqual(frozen.Preparation, d.preparation) {
		return nil
	}
	held, requested := frozen.Preparation, d.preparation
	if held != nil && requested != nil {
		definition, ok := d.preparationDefinition(held.Kind)
		if held.Command == string(reconciliation.Apply) && ok && definition.Automatic != nil &&
			requested.Kind == held.Kind && requested.Command == definition.Command {
			if samePreparationEffects(held, requested) {
				return nil
			}
			exact := *held
			exact.Command = definition.Command
			return prepareRefused.Fail("the targets and prerequisite selection must match the unfinished automatic preparation",
				preparationScopeCommand(operation.Context, exact, "--dry-run"))
		}
		return prepareRefused.Fail("this command or scope does not match the unfinished preparation", d.preparationCommand(operation.Context, frozen))
	}
	if held != nil {
		return prepareRefused.Fail("the unfinished operation is a frozen preparation and cannot become an installation", d.preparationCommand(operation.Context, frozen))
	}
	return prepareRefused.Fail("finish or remove the current operation before starting preparation", "continue its recorded command or destroy it with its original executable")
}

func samePreparationEffects(held, requested *reconciliation.PreparationScope) bool {
	return held.Kind == requested.Kind && held.PreparePrerequisites == requested.PreparePrerequisites && slices.Equal(held.Targets, requested.Targets)
}

func (s Service) withExplicitPreparation(scope *reconciliation.PreparationScope, preview bool) Service {
	definition, _ := s.preparationDefinition(scope.Kind)
	if definition.Automatic != nil {
		s.explicitPreparation = scope
		s.previewPreparation = preview
	}
	return s
}

func (s Service) preparationRetry(name, fallback string, flags ...string) string {
	if s.explicitPreparation == nil {
		return fallback
	}
	if s.previewPreparation && !slices.Contains(flags, "--dry-run") {
		flags = append(flags, "--dry-run")
	}
	return preparationScopeCommand(name, *s.explicitPreparation, flags...)
}

func (d decider) preparationCommand(name string, plan reconciliation.Plan) string {
	if plan.Preparation == nil {
		return contextCommand(name, string(plan.Verb), authorizing(plan)...)
	}
	definition, ok := d.preparationDefinition(plan.Preparation.Kind)
	if !ok {
		return "use the executable that registered this preparation"
	}
	if plan.Preparation.Command == string(reconciliation.Apply) {
		return contextCommand(name, string(reconciliation.Apply))
	}
	explicit := *plan.Preparation
	explicit.Command = definition.Command
	return preparationScopeCommand(name, explicit)
}

func (d decider) preparationAddition(ctx context.Context, view View, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, proveCustody bool) (*additiveAdmission, error) {
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone || (frozen.Preparation == nil && d.preparation == nil) {
		return nil, nil
	}
	if incomplete, err := unfinishedOver(ctx, store, operation, frozen, states); err != nil {
		return nil, err
	} else if len(incomplete) != 0 {
		return nil, refusal(prepareRefused.Fail("preparation has incomplete effects", unfinishedApplyExits(view)))
	}
	state, err := d.sources.compile(ctx, view)
	if err != nil {
		return nil, err
	}
	var before *compilation.State
	if inputDrift(view, operation) != "" {
		previous, _, ok := d.compiledBoth(ctx, view, operation)
		if !ok {
			return nil, refusal(prepareRefused.Fail("the preparation input cannot be compared with its recorded revision", d.changeRemedy(view)))
		}
		before = previous
	}
	if err := d.planner.refuseUnsupported(state, d.preparation); err != nil {
		return nil, refusal(err)
	}
	fresh, binding, err := d.planner.planFrom(ctx, view, state, d.preparation)
	if err != nil {
		return nil, verdict(err)
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return nil, err
	}
	owned, err := chain.plan()
	if err != nil {
		return nil, err
	}
	delta := reconciliation.Compare(owned, fresh)
	if first, changed := delta.First(); changed && first.Change != reconciliation.Added {
		return nil, refusal(prepareRefused.Fail(fmt.Sprintf("preparation transition would change owned block %s", first.Block), d.changeRemedy(view)))
	}
	if delta.Count(reconciliation.Added) == 0 {
		return nil, nil
	}
	scope := frozen.Preparation
	if d.preparation != nil {
		scope = d.preparation
	}
	definition, found := d.preparationDefinition(scope.Kind)
	if !found || !validPreparationDefinition(definition) || !definition.admitsCommand(scope.Command) {
		return nil, refusal(prepareUnsupported.Fail("the preparation owner is not registered or is incomplete", ""))
	}
	if err := definition.Advance(ctx, PreparationAdvance{AppliedEvidence: d.planner.planInput(view, state, binding.controller, d.preparation).AppliedEvidence, Scope: *scope, Context: view.Identity(), State: state, Before: before, Owned: owned, Fresh: fresh}); err != nil {
		return nil, verdict(err)
	}
	plans := make([]reconciliation.Plan, 0, len(chain))
	for _, member := range chain {
		plans = append(plans, member.plan)
	}
	added, err := reconciliation.Additive(fresh, plans...)
	if err != nil {
		return nil, err
	}
	merged, err := reconciliation.Lineage(append(plans, added)...)
	if err != nil {
		return nil, err
	}
	admission := &additiveAdmission{plan: added, merged: merged, secrets: binding.secrets, controller: binding.controller, preparedBinding: &binding}
	if proveCustody {
		if err := d.proofs.custodyProved(ctx, view, chain, added); err != nil {
			return nil, err
		}
	}
	return admission, nil
}
