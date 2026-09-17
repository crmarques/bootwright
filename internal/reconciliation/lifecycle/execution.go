package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

func (s Service) Apply(ctx context.Context, request ApplyRequest) (*OperationResult, error) {
	selection, err := reconciliation.ParseStages(request.Stages)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, reconciliation.Apply, request.ContextName, selection, request.Authorizations, request.SkipConfirmation, request.SSH.Borrowed())
}

// Destroy removes what an apply recorded as owned. It accepts no stage
// selection: a removal covers exactly the effects that exist.
func (s Service) Destroy(ctx context.Context, request DestroyRequest) (*OperationResult, error) {
	return s.mutate(ctx, reconciliation.Destroy, request.ContextName, nil, request.Authorizations, request.SkipConfirmation, request.SSH.Borrowed())
}

// transition is the single legal next step the durable state permits. A noop
// transition is the legal step that has nothing left to do: the context
// already holds the state its verb would leave it in.
type transition struct {
	fresh     bool
	noop      bool
	verb      reconciliation.Verb
	operation operationstore.Operation
	plan      reconciliation.Plan
	binding   capabilityBinding
	source    string
	// reopen is the binding a fresh removal inherits: the one its apply froze,
	// so a removal presents the material that created what it removes rather
	// than whatever the current declarations name.
	reopen    string
	release   []string
	states    map[string]reconciliation.BlockState
	selection reconciliation.StageSelection
}

func (s Service) mutate(ctx context.Context, verb reconciliation.Verb, contextName string, selection reconciliation.StageSelection, authorizations []string, skipConfirmation, borrowed bool) (*OperationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if borrowed {
		return nil, failure("lifecycle.state",
			"borrowed SSH credentials are unsupported for lifecycle operations",
			"remove --ssh-user, --ssh-id-file and --ssh-ask-sudo-password and author the Machine's own access")
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	var decided transition
	var identity ContextIdentity
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		identity = view.Identity()
		decided, err = s.decide(ctx, view, verb, selection)
		return err
	})
	if err != nil {
		return nil, err
	}
	// A verb with nothing to do ends here. It registers nothing and performs
	// no effect, so it needs neither authorization nor confirmation: there is
	// no consequence to acknowledge and nothing a habitual token could
	// pre-authorize, and repeating a completed verb stays safe.
	if decided.noop {
		return settled(identity, decided), nil
	}
	if err := authorize(decided.plan, authorizations); err != nil {
		return nil, err
	}
	if err := s.present(ctx, name, decided); err != nil {
		return nil, err
	}
	if !skipConfirmation {
		if s.options.Confirmer == nil {
			return nil, failure("lifecycle.state", "this operation requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, string(verb), name); err != nil {
			return nil, err
		}
	}
	return s.execute(ctx, name, decided)
}

// authorize compares the tokens this invocation supplied with the tokens the
// frozen plan's blocks consume. A missing token refuses before registration, so
// an irreversible consequence is always acknowledged first; a token the plan
// does not consume refuses too, so a habitual authorization cannot
// pre-authorize a future destructive plan.
func authorize(plan reconciliation.Plan, authorizations []string) error {
	consumers := map[string][]string{}
	required := []string{}
	for _, block := range plan.Blocks {
		for _, token := range block.Consumes {
			if !slices.Contains(required, token) {
				required = append(required, token)
			}
			consumers[token] = append(consumers[token], block.ID)
		}
	}
	supplied := slices.Compact(slices.Sorted(slices.Values(authorizations)))
	for _, token := range supplied {
		if !slices.Contains(required, token) {
			return failure("lifecycle.authorization",
				"this plan requires no "+token+" authorization",
				"repeat the command without --authorize "+token)
		}
	}
	for _, token := range required {
		if !slices.Contains(supplied, token) {
			return failure("lifecycle.authorization",
				"this plan has "+token+" consequences that are not authorized: "+strings.Join(consumers[token], ", "),
				"review the plan's impacts and repeat the command with --authorize "+token)
		}
	}
	return nil
}

// decide reads durable state and returns the one legal transition. Changed
// desired state never turns a continuation into a reconciliation.
func (s Service) decide(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.StageSelection) (transition, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return transition{}, err
	}
	if index.Current == "" {
		if verb == reconciliation.Destroy {
			return transition{noop: true, verb: verb}, nil
		}
		return s.freshApply(ctx, view, selection)
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return transition{}, err
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return transition{}, err
	}
	states, err := store.BlockStates(ctx, operation.ID, frozen)
	if err != nil {
		return transition{}, err
	}
	if verb == reconciliation.Destroy && supersedable(operation) {
		return s.supersede(ctx, view, store, operation, frozen, states)
	}
	if operation.State != reconciliation.OperationDone {
		if operation.Verb != verb {
			return transition{}, failure("lifecycle.state",
				"an incomplete "+string(operation.Verb)+" must be continued before another operation",
				"run "+string(operation.Verb)+" to continue it")
		}
		if verb == reconciliation.Apply {
			if err := refuseStageBoundary(frozen, states, selection); err != nil {
				return transition{}, err
			}
		}
		decided := transition{verb: verb, operation: operation, plan: frozen, states: states, source: operation.Source, selection: selection}
		if verb == reconciliation.Destroy && operation.Source != "" {
			applied, err := store.ReadOperation(ctx, operation.Source)
			if err != nil {
				return transition{}, err
			}
			decided.release = applied.Bindings
		}
		return decided, nil
	}
	if operation.Verb == reconciliation.Apply {
		if verb == reconciliation.Apply {
			if unchangedInput(view, operation) {
				return transition{noop: true, verb: verb, operation: operation, plan: frozen, states: states}, nil
			}
			return transition{}, failure("lifecycle.state",
				"the desired state changed after this apply completed",
				"destroy what it owns before applying the changed input")
		}
		return s.freshDestroy(ctx, operation.Executable, operation.ID, firstBinding(operation.Bindings),
			operation.Bindings, reconciliation.OwnedSubset(frozen, states))
	}
	if verb == reconciliation.Destroy {
		return transition{noop: true, verb: verb, operation: operation, plan: frozen, states: states}, nil
	}
	return s.freshApply(ctx, view, selection)
}

// unchangedInput reports whether this context still holds exactly the desired
// state its completed operation froze. That equality is what makes repeating
// the verb a no-op rather than a request to realize something else.
func unchangedInput(view View, operation operationstore.Operation) bool {
	identity := view.Identity()
	return operation.Context == identity.Name &&
		operation.Revision == identity.Revision &&
		operation.InputDigest == inputDigest(view)
}

// settled reports the verb whose work durable state already proves. It touches
// no record, so the operation it names keeps the state and identity it
// finished with.
func settled(identity ContextIdentity, decided transition) *OperationResult {
	operation := decided.operation.ID
	if operation == "" {
		operation = "none"
	}
	return &OperationResult{
		Context: identity,
		Verb:    string(decided.verb),
		Steps:   steps(decided.plan, decided.states),
		Blocks:  blockResults(decided.plan, decided.states),
		Settled: true,
		Receipt: Receipt{
			Operation: operation, Verb: string(decided.verb),
			State: string(reconciliation.OperationDone), Next: "none",
		},
	}
}

// refuseStageBoundary refuses before any effect when the selected stages admit
// no work. It names the stage that would unblock the operation, so a selection
// mistake is corrected rather than silently doing nothing.
func refuseStageBoundary(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.StageSelection) error {
	for _, block := range plan.Blocks {
		switch states[block.ID] {
		case reconciliation.BlockUnknown, reconciliation.BlockRunning:
			return nil
		}
	}
	for _, block := range plan.Blocks {
		if states[block.ID] != reconciliation.BlockFailed {
			continue
		}
		if selection.Selects(block.Stage) {
			return nil
		}
		return failure("lifecycle.stage",
			"the block this operation must retry is outside the selected stages",
			"repeat the operation including --stage "+string(block.Stage))
	}
	if len(reconciliation.Startable(plan, states, selection)) != 0 {
		return nil
	}
	ready := reconciliation.Ready(plan, states)
	if len(ready) == 0 {
		return failure("lifecycle.stage", "the selected stages have nothing to start", "repeat the operation without --stage")
	}
	return failure("lifecycle.stage",
		"the selected stages have nothing to start",
		"repeat the operation including --stage "+string(ready[0].Stage))
}

func (s Service) freshApply(ctx context.Context, view View, selection reconciliation.StageSelection) (transition, error) {
	state, err := s.compile(ctx, view)
	if err != nil {
		return transition{}, err
	}
	if err := s.refuseUnsupported(state); err != nil {
		return transition{}, err
	}
	plan, binding, err := s.planFrom(ctx, view, state, reconciliation.Apply)
	if err != nil {
		return transition{}, err
	}
	if len(plan.Blocks) == 0 {
		return transition{}, failure("lifecycle.state", "the selected Environment declares nothing this executable would create", "declare a managed infrastructure service, or see "+supportedExample)
	}
	if err := refuseStageBoundary(plan, nil, selection); err != nil {
		return transition{}, err
	}
	return transition{fresh: true, verb: reconciliation.Apply, plan: plan, binding: binding, selection: selection}, nil
}

// supersedable reports whether a fresh removal may replace an incomplete
// operation. A pause is a resumable boundary and a failure is a retry point;
// neither holds an unproved effect, so what the context still owns is exactly
// derivable from the frozen plan. A failed operation needs this road because
// its continuation is frozen to the automation it registered under: repairing
// the very adapter that failed it would otherwise leave no way out.
func supersedable(operation operationstore.Operation) bool {
	switch operation.State {
	case reconciliation.OperationPaused:
		return operation.Verb == reconciliation.Apply
	case reconciliation.OperationFailed:
		return true
	}
	return false
}

// supersede plans a fresh removal over what an incomplete operation still owns:
// the blocks an apply started, or the blocks a removal has not yet proved gone.
func (s Service) supersede(ctx context.Context, view View, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (transition, error) {
	if operation.Verb == reconciliation.Apply {
		return s.freshDestroy(ctx, operation.Executable, operation.ID, firstBinding(operation.Bindings),
			operation.Bindings, reconciliation.OwnedSubset(frozen, states))
	}
	// A superseded removal is continued by nothing, so its own binding is
	// released beside the apply's once the replacement completes. The one it
	// reopens is the apply's, because that is the material the effects it takes
	// back were created with.
	release := slices.Clone(operation.Bindings)
	reopen := firstBinding(operation.Bindings)
	if operation.Source != "" {
		applied, err := store.ReadOperation(ctx, operation.Source)
		if err != nil {
			return transition{}, err
		}
		for _, binding := range applied.Bindings {
			if !slices.Contains(release, binding) {
				release = append(release, binding)
			}
		}
		if inherited := firstBinding(applied.Bindings); inherited != "" {
			reopen = inherited
		}
	}
	return s.freshDestroy(ctx, operation.Executable, operation.Source, reopen, release,
		reconciliation.RemainingSubset(frozen, states))
}

func firstBinding(bindings []string) string {
	if len(bindings) == 0 {
		return ""
	}
	return bindings[0]
}

// freshDestroy plans removal from the plan the operation being removed froze,
// so a build whose derivation has moved since still removes exactly the effects
// that exist. It reads no desired state: the frozen requests are the effects,
// and this executable only reads them.
func (s Service) freshDestroy(ctx context.Context, frozenBy operationstore.Executable, source, reopen string, release []string, owned reconciliation.Plan) (transition, error) {
	// A context that owns nothing settles before it reaches here, so an empty
	// owned set means the operation this removal supersedes contradicts its
	// own block records rather than that there is nothing to do.
	if len(owned.Blocks) == 0 {
		return transition{}, failure("lifecycle.state",
			"the operation this removal supersedes records no block it started",
			"review its durable state with bootwright status")
	}
	plan, err := s.removalOf(ctx, owned, frozenBy)
	if err != nil {
		return transition{}, err
	}
	return transition{
		fresh: true, verb: reconciliation.Destroy, plan: plan,
		source: source, reopen: reopen, release: release,
	}, nil
}

// removalOf turns the frozen blocks an operation still owns into the plan that
// removes them. Every block keeps the identity, implementation, content digest
// and request its apply froze; its capability supplies only what removing it
// decides, so the plan describes the effects that exist rather than the effects
// this executable would create from the same input today.
func (s Service) removalOf(ctx context.Context, owned reconciliation.Plan, executable operationstore.Executable) (reconciliation.Plan, error) {
	inverted, err := invertOwned(owned)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	definitions := make([]reconciliation.BlockDefinition, 0, len(inverted.Blocks))
	for _, block := range inverted.Blocks {
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !ok {
			return reconciliation.Plan{}, failure("lifecycle.state",
				"this executable provides no "+block.Implementation+" to remove the block "+block.ID+" with",
				removeWith(executable))
		}
		removal, err := capability.Removal(ctx, block)
		if err != nil {
			return reconciliation.Plan{}, unreadable(block, executable, err)
		}
		definition := block.BlockDefinition
		definition.Dependencies = slices.Clone(definition.Dependencies)
		// Requirements are already resolved into the frozen dependencies this
		// removal inverted, and a subset may no longer contain the block a
		// requirement names.
		definition.Requires = nil
		definition.Description, definition.Impacts = removal.Description, removal.Impacts
		definition.Consumes, definition.Groups = removal.Consumes, removal.Groups
		definitions = append(definitions, definition)
	}
	return reconciliation.NewPlan(reconciliation.Destroy, definitions)
}

// invertOwned shapes the owned set as a removal. An apply's plan is inverted so
// every block waits on its own dependents; a removal's own plan already is that
// inverse and is only narrowed, which drops the edges to blocks it no longer
// carries.
func invertOwned(owned reconciliation.Plan) (reconciliation.Plan, error) {
	if owned.Verb == reconciliation.Apply {
		return owned.Inverse()
	}
	identities := make([]string, 0, len(owned.Blocks))
	for _, block := range owned.Blocks {
		identities = append(identities, block.ID)
	}
	return owned.Retain(identities)
}

// unreadable reports a frozen block this executable cannot read, naming what it
// holds and the executable that wrote it, so the remedy is a command rather
// than the obstacle that stopped it.
func unreadable(block reconciliation.Block, executable operationstore.Executable, err error) error {
	detail := ""
	if reported := diagnostics.Of(err); len(reported) != 0 {
		detail = ": " + reported[0].Message
	}
	return failure("lifecycle.state",
		"the request the block "+block.ID+" froze cannot be read by this executable"+detail,
		removeWith(executable))
}

// removeWith names the executable that registered what is being removed. An
// operation recorded before that identity existed names none, and the operator
// is pointed at the record instead.
func removeWith(executable operationstore.Executable) string {
	if executable.Version == "" {
		return "remove it with the executable its operation.json records"
	}
	identity := executable.Version
	if executable.Commit != "" {
		identity += " (" + executable.Commit + ")"
	}
	return "remove it with bootwright " + identity
}

func (s Service) present(ctx context.Context, name string, decided transition) error {
	if s.options.Presenter == nil {
		return failure("lifecycle.state", "lifecycle plan presentation is not configured", "")
	}
	result := planPreview(decided.plan, decided.states, decided.selection)
	result.Context = ContextIdentity{Name: name}
	result.Verb = string(decided.verb)
	result.Continuation = !decided.fresh
	result.Receipt = Receipt{Operation: "none", Verb: string(decided.verb), State: "preview", Next: string(decided.verb)}
	if !decided.fresh {
		result.Receipt.Operation = decided.operation.ID
		result.Receipt.Next = "continue-" + string(decided.verb)
	}
	return s.options.Presenter.PresentLifecyclePlan(ctx, result)
}

// execute binds the Secrets the plan consumes, then performs the operation
// inside one held transaction. Binding happens first because acquiring
// confidential material takes the same store lock the transaction holds.
func (s Service) execute(ctx context.Context, name string, decided transition) (*OperationResult, error) {
	binding, material, err := s.bind(ctx, name, decided)
	if err != nil {
		return nil, err
	}
	defer clearMaterial(material)
	var result *OperationResult
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		result, err = s.run(ctx, tx, decided, binding, material)
		return err
	})
	if err != nil {
		// Only a failed registration releases what it just bound, and only when
		// it bound it: a removal inherits its apply's binding, and releasing
		// that would leave a context whose effects no later removal can ever
		// present the material for. Once the operation is registered it owns
		// its binding for its whole lifetime, because every later attempt
		// reopens it; releasing here would leave a durable operation that can
		// never be continued.
		if decided.fresh && decided.reopen == "" && binding != "" && result == nil {
			_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
		}
		return result, err
	}
	// A completed removal no longer needs the material its apply bound.
	if decided.verb == reconciliation.Destroy && result != nil && result.Receipt.State == string(reconciliation.OperationDone) {
		for _, released := range slices.Clone(decided.release) {
			_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: released})
		}
	}
	return result, nil
}

func (s Service) bind(ctx context.Context, name string, decided transition) (string, map[string]secrets.Material, error) {
	references := decided.binding.secrets
	// A removal inherits its apply's binding, so it neither re-binds current
	// declarations nor needs the plan to have named any.
	if !decided.fresh || decided.reopen != "" {
		references = nil
	}
	identity := ""
	if len(references) != 0 {
		result, err := s.binder.Bind(ctx, custody.BindRequest{ContextName: name, Names: references})
		if err != nil {
			return "", nil, err
		}
		identity = result.ID
	}
	if decided.reopen != "" {
		identity = decided.reopen
	}
	if !decided.fresh && len(decided.operation.Bindings) != 0 {
		identity = decided.operation.Bindings[0]
	}
	if identity == "" {
		return "", map[string]secrets.Material{}, nil
	}
	bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: identity})
	if err != nil {
		return identity, nil, err
	}
	material := make(map[string]secrets.Material, len(bound))
	for _, item := range bound {
		material[item.Version.Declaration.Name] = item.Material
	}
	return identity, material, nil
}

func clearMaterial(material map[string]secrets.Material) {
	for _, value := range material {
		value.Clear()
	}
}

// run performs the whole operation under one held transaction: registration,
// reservations, sequential block execution and the evidence projection.
func (s Service) run(ctx context.Context, tx Transaction, decided transition, binding string, material map[string]secrets.Material) (*OperationResult, error) {
	store := s.store(tx)
	// A removal proves every asset it would take back is out of use before it
	// registers. A per-effect check alone would not do: a removal takes
	// dependents first, so it would delete the quiescent leaves and then stop
	// at the running machine, leaving a context that can only continue the
	// destroy it should never have started.
	if decided.fresh && decided.verb == reconciliation.Destroy {
		if err := s.proveQuiescent(ctx, tx, decided.plan, material); err != nil {
			return nil, err
		}
	}
	operation, plan, err := s.register(ctx, tx, store, decided, binding)
	if err != nil {
		return nil, err
	}
	result := &OperationResult{Context: tx.Identity(), Verb: string(operation.Verb), Steps: steps(plan, nil)}
	log, err := store.OpenLog(ctx, operationstore.OperationLogPath(operation.ID))
	if err != nil {
		return result, logFault(err)
	}
	defer func() { _ = log.Close(ctx) }()
	// The location is named before the first effect, because its whole purpose
	// is to be followed while the work runs.
	result.LogLocation = store.LogDirectory(operation.ID)
	if s.options.Progress != nil && result.LogLocation != "" {
		s.options.Progress.ReportLogLocation(ctx, result.LogLocation)
	}
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		return result, err
	}
	// Every attempt of this operation runs inside one approved bundle, so it is
	// opened once here rather than by each attempt in turn.
	approved, err := approvedBundle(ctx, tx)
	if err != nil {
		return result, err
	}
	boundary, cause := s.converge(ctx, tx, store, approved, operation, plan, states, material, decided.selection)
	final, terminal := s.finish(recordingContext(ctx), tx, store, operation, plan, states, boundary, result)
	return final, withCause(cause, terminal)
}

// The gate is one step, and the blocks it probes are that step's sub-steps, so
// a removal of any size settles the whole proof as one row and the rows that
// follow are the plan's effects alone.
const (
	quiescenceCheck            = "quiescence"
	quiescenceCheckDescription = "prove nothing this removal takes back is still in use"
)

// proveQuiescent observes every block a removal would take back and refuses
// while any of it is still in use. Every block is probed rather than the first
// live one alone, so an operator is told everything to stop instead of
// discovering the next obstacle each time they repeat the command. A probe
// that cannot read its target reports live, because an environment that cannot
// prove it is idle is never assumed to be.
func (s Service) proveQuiescent(ctx context.Context, tx Transaction, plan reconciliation.Plan, material map[string]secrets.Material) error {
	approved, err := approvedBundle(ctx, tx)
	if err != nil {
		return err
	}
	var live, stops []string
	err = s.guard.WithPython(ctx, approved.area, approved.requirement, func(launch prerequisites.PythonLaunch, _ func() error) error {
		for index, block := range plan.Blocks {
			if err := ctx.Err(); err != nil {
				return err
			}
			capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
			if !ok {
				return failure("lifecycle.state",
					"this executable does not offer the implementation this block froze",
					"install the executable that registered this operation")
			}
			s.reportCheck(ctx, ProgressEvent{
				Group: block.ID, Detail: block.Kind + "/" + block.Object, Status: "running",
				Completed: index, Declared: len(plan.Blocks),
			})
			state, err := capability.Quiescent(ctx, Probe{
				Block: block, Launch: launch, Bundle: approved.location, Area: approved.area, Material: material,
			})
			if err != nil {
				return err
			}
			if state.Settled() {
				continue
			}
			live = append(live, block.Kind+"/"+block.Object+" ("+state.Reason+")")
			if state.Stop != "" && !slices.Contains(stops, state.Stop) {
				stops = append(stops, state.Stop)
			}
		}
		return nil
	})
	if err != nil {
		// A probe that never answered leaves the gate nothing to claim.
		s.reportCheck(ctx, ProgressEvent{Status: "unknown"})
		return err
	}
	if len(live) == 0 {
		s.reportCheck(ctx, ProgressEvent{Status: "ok"})
		return nil
	}
	s.reportCheck(ctx, ProgressEvent{Status: "failed"})
	remediation := "stop what is running, then repeat the removal"
	if len(stops) != 0 {
		remediation = "stop it with " + strings.Join(stops, ", then ")
	}
	return failure("lifecycle.live", "this removal would take back state that is still in use: "+strings.Join(live, ", "), remediation)
}

// reportCheck streams one row of the gate. It runs before the operation
// exists, so it belongs to no block and counts against no frozen total.
func (s Service) reportCheck(ctx context.Context, event ProgressEvent) {
	event.Phase, event.Block, event.Description = CheckPhase, quiescenceCheck, quiescenceCheckDescription
	s.report(ctx, event)
}

// withCause keeps the block's own diagnostics beside the terminal state. The
// terminal failure alone says only that the operation did not complete, which
// is never the reason an operator needs. A cause that carries no diagnostics,
// such as a cancellation, is left for the boundary that recognizes it.
func withCause(cause, terminal error) error {
	reported := diagnostics.Of(cause)
	if len(reported) == 0 {
		return terminal
	}
	return &diagnostics.Failure{Diagnostics: append(reported, diagnostics.Of(terminal)...)}
}

// unproved covers both ways an effect loses its outcome: an attempt that
// reported unknown, and one whose executor died before it reported anything.
func unproved(state reconciliation.BlockState) bool {
	return state == reconciliation.BlockUnknown || state == reconciliation.BlockRunning
}

func pendingRemains(plan reconciliation.Plan, states map[string]reconciliation.BlockState) bool {
	for _, block := range plan.Blocks {
		if states[block.ID] != reconciliation.BlockDone {
			return true
		}
	}
	return false
}

func (s Service) register(ctx context.Context, tx Transaction, store OperationStore, decided transition, binding string) (operationstore.Operation, reconciliation.Plan, error) {
	if !decided.fresh {
		operation, err := store.ReadOperation(ctx, decided.operation.ID)
		if err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		plan, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		if err := s.verifyContinuation(ctx, tx, operation); err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
		if operation.State != reconciliation.OperationRunning {
			operation.State = reconciliation.OperationRunning
			if err := store.UpdateOperation(ctx, operation); err != nil {
				return operationstore.Operation{}, reconciliation.Plan{}, err
			}
		}
		return operation, plan, nil
	}
	// A fresh apply is where a context claims its controller host. Binding
	// precedes every reservation and effect, so an operation never leaves work
	// behind on a host the context is not recorded against.
	if decided.verb == reconciliation.Apply {
		if err := s.establishBinding(ctx, tx, decided.binding.controller); err != nil {
			return operationstore.Operation{}, reconciliation.Plan{}, err
		}
	}
	if err := s.reserve(ctx, tx, decided); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	index, err := store.Index(ctx)
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	identity, err := reconciliation.AllocateOperationID(s.options.Entropy, func(candidate string) bool { return candidate == index.Current })
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	digest, err := decided.plan.Digest()
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	stamp := s.options.Clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00")
	bindings := []string{}
	if binding != "" {
		bindings = append(bindings, binding)
	}
	operation := operationstore.Operation{
		Version: 1, ID: identity, Verb: decided.verb,
		Context: tx.Identity().Name, Revision: tx.Identity().Revision,
		InputDigest: inputDigest(tx), PlanDigest: digest, AutomationDigest: s.automation.CatalogDigest(),
		Executable: operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit},
		Source:     decided.source, Bindings: bindings, State: reconciliation.OperationRunning,
		Created: stamp, Updated: stamp,
	}
	if err := store.Register(ctx, operation, decided.plan); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	if err := s.project(ctx, tx, decided.verb, reconciliation.OperationRunning); err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, err
	}
	return operation, decided.plan, nil
}

// reserve claims the exclusive host resources the plan needs before any effect.
// A destroy needs no new claim; it releases at completion.
func (s Service) reserve(ctx context.Context, tx Transaction, decided transition) error {
	if decided.verb == reconciliation.Destroy {
		return nil
	}
	return tx.Reserve(ctx, decided.binding.reservations)
}

// establishBinding proves the prepared host and records this context against
// it. Setup prepares a host without claiming any context, so the relationship
// is published here, once, by the operation that first uses the host.
func (s Service) establishBinding(ctx context.Context, tx Transaction, machine string) error {
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	view := tx.Controller()
	if !view.Exists || !view.Initialized || view.State.Receipt.Status != "complete" {
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright setup")
	}
	return tx.Bind(ctx, machine, host)
}

// verifyContinuation re-proves everything a continuation depends on before it
// does work. Drift refuses; it never re-resolves to another implementation.
func (s Service) verifyContinuation(ctx context.Context, tx Transaction, operation operationstore.Operation) error {
	identity := tx.Identity()
	if operation.Context != identity.Name || operation.Revision != identity.Revision {
		return failure("lifecycle.state", "the context input changed after this operation registered", "restore the exact input revision this operation froze")
	}
	if operation.InputDigest != inputDigest(tx) {
		return failure("lifecycle.state", "the frozen input no longer matches this operation", "restore the exact input revision this operation froze")
	}
	if operation.AutomationDigest != s.automation.CatalogDigest() {
		remediation := "install the compatible executable and run bootwright setup"
		if supersedable(operation) {
			remediation = "destroy what this operation owns under this executable, or install the one it registered under"
		}
		return failure("lifecycle.state", "this executable's automation differs from the one this operation froze", remediation)
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	return verifyHostBinding(tx.Controller(), identity, host)
}

// verifyHostBinding proves this context is bound to the host the operation
// runs on and that its setup completed, before any local effect.
func verifyHostBinding(view prerequisites.StorageView, identity ContextIdentity, host controller.InstalledHostIdentity) error {
	if !view.Exists || !view.Initialized {
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright setup")
	}
	if view.State.Receipt.Status != "complete" {
		return failure("controller.state", "the retained controller setup is incomplete", "run bootwright setup to resolve it")
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		return err
	}
	if !view.State.Host.Equal(host) {
		return failure("controller.identity", "this host is not the host the context is bound to", "restore the original host, or create a context on this one")
	}
	for _, binding := range view.State.Bindings {
		if binding.Context != identity.Name {
			continue
		}
		if binding.HostDigest != digest {
			return failure("controller.identity", "the recorded controller binding does not match this host", "restore the original host, or create a context on this one")
		}
		return nil
	}
	return failure("controller.identity", "this context is not bound to a controller host", "run bootwright apply to bind it")
}

// inputDigest binds the operation to the exact frozen bytes it planned from,
// independently of the revision identity the registry records.
func inputDigest(view View) string {
	sources := view.Inputs()
	parts := make([]string, 0, len(sources.Files)+len(sources.Markers))
	for _, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		for _, file := range collection {
			content := sha256.Sum256(file.Bytes())
			parts = append(parts, file.Path()+"\x00"+hex.EncodeToString(content[:]))
		}
	}
	slices.Sort(parts)
	digest := sha256.Sum256([]byte("bootwright.reconciliation.input-v1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
