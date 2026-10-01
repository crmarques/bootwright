package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

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
	fresh bool
	noop  bool
	// finalize marks an operation whose block records prove it complete while
	// its record, evidence, reservations or Secret bindings do not yet say so.
	finalize bool
	// unclaimed marks a destroy of a context holding no operation whose
	// running evidence or reservations an interrupted registration left.
	unclaimed bool
	// idle marks a destroy that settles beside a claim that holds nothing,
	// which only a transaction of its own reclaims.
	idle      bool
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
	// basis is the durable state this transition was planned from. For a fresh
	// removal it is the incomplete operation the removal takes the place of,
	// which is the context's current operation. It is not always source: a
	// removal that replaces a failed removal inherits that one's apply as its
	// source while replacing the removal itself.
	basis basis
}

// basis is the durable state a transition was planned from: the context's
// current operation, its whole record, and each of its frozen blocks' state and
// attempt count, or no operation at all. The frozen plan is part of it through
// the record's plan digest, which every read of the plan is held to. The
// decision is taken under the shared lock and the effects run under the
// exclusive one, so the transition re-proves this before it does anything.
//
// The apply a removal releases is not part of it. That apply is either the
// basis operation itself, for a fresh removal over an apply, or one that
// stopped being current when its first removal registered, and it is written
// only by the restoration and the resolutions recorded in the transaction that
// registers that removal, so nothing moves it between a later decision and its
// effects.
type basis struct {
	operation string
	state     reconciliation.OperationState
	record    operationstore.Operation
	blocks    map[string]reconciliation.BlockState
	attempts  map[string]int
	// revision and input are the input revision and digest a fresh apply
	// compiled its plan from. Every other transition plans from frozen records
	// rather than from the input, so it leaves them empty.
	revision string
	input    string
	// evidence is the mutation evidence the transition requires the context
	// to still hold: the running evidence a fresh apply raised, or what a
	// destroy of a context holding no operation decided to release.
	evidence []byte
	// claimed is the operation directory a fresh apply claimed, and claims the
	// operation directories right after that claim. A reclaim removes one only
	// while the evidence reads pristine, or once a registration moved the
	// index, which refuses this apply on its own, so a claim that raised the
	// evidence again after a release lowered it is still listed, even under
	// evidence that reads the same bytes again. The set is compared rather
	// than its size, because a reclaim of an older claim and that newer claim
	// leave the same number.
	claimed string
	claims  []string
}

// describe names the operation a basis holds and that operation's state, in
// the words a refusal reports them with.
func (b basis) describe() string {
	if b.operation == "" {
		return "no operation"
	}
	return "operation " + b.operation + " (" + string(b.state) + ")"
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
	// A destroy lists the context's bindings before it decides, so one that
	// settles releases only what existed before it read pristine evidence.
	var stranded []string
	if verb == reconciliation.Destroy {
		stranded = s.held(ctx, name)
	}
	decided, identity, err := s.decideShared(ctx, name, verb, selection)
	if err != nil {
		return nil, err
	}
	// An operation its block records already prove complete is finalized
	// before anything is authorized, presented or confirmed, because doing so
	// performs no effect: only the record, releases and projection those
	// records prove. The verb then decides again from what that leaves.
	finalized := decided.finalize
	if decided.finalize {
		if decided, identity, err = s.finalizeFirst(ctx, name, verb, selection, decided); err != nil {
			return nil, err
		}
	}
	// What an interrupted registration left with no operation to own it is
	// released the same way, for the same reason: it performs no effect, only
	// the releases and the pristine evidence that no operation's records need.
	if decided.unclaimed {
		if err := s.releaseUnclaimed(ctx, name, decided); err != nil {
			return nil, err
		}
		return settled(identity, transition{verb: verb}, RecoveredRelease), nil
	}
	// A verb with nothing to do ends here. It registers nothing and performs
	// no effect, so it needs neither authorization nor confirmation: there is
	// no consequence to acknowledge and nothing a habitual token could
	// pre-authorize, and repeating a completed verb stays safe. A finalization
	// this invocation completed first is the only thing it did, and it says so.
	//
	// A destroy settles only over pristine evidence and no reservation, where
	// no operation names a binding, so every binding it listed before it read
	// that evidence is stranded and released, still without a transaction. An
	// apply in flight that bound one of them raised the evidence before it
	// bound, so a release lowered it since and that apply refuses at its
	// re-proof rather than register the binding. A removal's finalization this
	// invocation completed already collected what it listed after this one.
	// A claim that holds nothing beside it is what an apply stopped before its
	// running evidence landed leaves, and it is reclaimed in a transaction that
	// does nothing else.
	if decided.noop {
		if decided.idle {
			s.reclaimIdle(ctx, name)
		}
		if verb == reconciliation.Destroy && !finalized {
			s.collect(ctx, name, stranded, nil)
		}
		recovered := ""
		if finalized {
			recovered = RecoveredFinalization
		}
		return settled(identity, decided, recovered), nil
	}
	if err := s.refuseLostBinding(ctx, name, decided, decided.basis); err != nil {
		return nil, err
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

// decideShared takes one decision from durable state read under the shared
// lock, together with the identity of the context it read.
func (s Service) decideShared(ctx context.Context, name string, verb reconciliation.Verb, selection reconciliation.StageSelection) (transition, ContextIdentity, error) {
	var decided transition
	var identity ContextIdentity
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		identity = view.Identity()
		decided, err = s.decide(ctx, view, verb, selection)
		return err
	})
	return decided, identity, err
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
// desired state never turns a continuation into a reconciliation. An operation
// whose finalization is due is marked before any other decision is taken.
func (s Service) decide(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.StageSelection) (transition, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return transition{}, err
	}
	if index.Current == "" {
		if verb == reconciliation.Destroy {
			return unclaimed(ctx, view, store)
		}
		// A lost index reads as no operation, and an apply registered over it
		// would leave the effects its records name to nothing.
		if err := refuseUnindexed(ctx, view, store); err != nil {
			return transition{}, err
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
	states, attempts, err := blockRecords(ctx, store, operation.ID, frozen)
	if err != nil {
		return transition{}, err
	}
	if marked, err := finalization(ctx, view, store, verb, operation, frozen, states, attempts); err != nil || marked.finalize {
		return marked, err
	}
	return s.decideOver(ctx, view, store, verb, selection, operation, frozen, states, attempts)
}

// decideOver is the decision over a current operation whose finalization is
// not due. A preview also takes it over the record a due finalization would
// leave, so it decides as the verb does once that finalization is done.
func (s Service) decideOver(ctx context.Context, view View, store OperationStore, verb reconciliation.Verb, selection reconciliation.StageSelection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (transition, error) {
	if verb == reconciliation.Destroy && supersedable(operation) {
		return s.supersede(ctx, view, store, operation, frozen, states, attempts)
	}
	if operation.State != reconciliation.OperationDone {
		if operation.Verb != verb {
			return transition{}, failure("lifecycle.state",
				"an incomplete "+string(operation.Verb)+" must be continued before another operation",
				"run "+string(operation.Verb)+" to continue it")
		}
		if err := refuseUncontinuable(ctx, store, operation, frozen); err != nil {
			return transition{}, err
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
		return plannedFrom(decided, operation, states, attempts), nil
	}
	if operation.Verb == reconciliation.Apply {
		if verb == reconciliation.Apply {
			return settleApply(view, operation, frozen, states)
		}
		owned, err := completedOwnership(operation, frozen, states)
		if err != nil {
			return transition{}, err
		}
		decided, err := s.freshDestroy(ctx, operation.Executable, operation.ID, firstBinding(operation.Bindings),
			operation.Bindings, owned, false)
		if err != nil {
			return transition{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
	}
	if unfinished := unfinishedBlocks(frozen, states); len(unfinished) != 0 {
		return transition{}, failure("lifecycle.state",
			"the completed destroy "+operation.ID+" records no block completion for these blocks, so nothing proves their effects removed: "+strings.Join(unfinished, ", "),
			deletionExit(view))
	}
	if verb == reconciliation.Destroy {
		return settledDestroy(ctx, store, transition{noop: true, verb: verb, operation: operation, plan: frozen, states: states}), nil
	}
	decided, err := s.freshApply(ctx, view, selection)
	if err != nil {
		return transition{}, err
	}
	return plannedFrom(decided, operation, states, attempts), nil
}

// completedOwnership is what a completed apply owns: its whole frozen plan,
// because an apply completes only once every block of it is done. A block whose
// record reads anything else contradicts that completion, and a lost record is
// such a block, since absence reads back as pending. A removal of the done rest
// would leave that block's effect in place and then release the binding it
// needs, so the removal refuses instead, naming every such block, before it
// binds, probes, registers or releases anything. An incomplete apply and a
// failed removal legitimately hold blocks that are not done and never come here.
func completedOwnership(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (reconciliation.Plan, error) {
	if unfinished := unfinishedBlocks(frozen, states); len(unfinished) != 0 {
		return reconciliation.Plan{}, failure("lifecycle.state",
			"the completed apply "+operation.ID+" records no block completion for these blocks, and a removal that skipped one would leave its effect in place: "+strings.Join(unfinished, ", "),
			"review its durable state with bootwright status")
	}
	return reconciliation.OwnedSubset(frozen, states), nil
}

// settleApply is an apply over a completed apply. A changed input refuses by
// naming the removal it needs. The unchanged input settles only while every
// block of the frozen plan is done, because the settled verb reports that
// completion: a block whose record reads anything else, a lost one included,
// contradicts it, and nothing then proves the input applied.
func settleApply(view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (transition, error) {
	if !unchangedInput(view, operation) {
		return transition{}, failure("lifecycle.state",
			"the desired state changed after this apply completed",
			"destroy what it owns before applying the changed input")
	}
	if unfinished := unfinishedBlocks(frozen, states); len(unfinished) != 0 {
		return transition{}, failure("lifecycle.state",
			"the completed apply "+operation.ID+" records no block completion for these blocks, so this input cannot be proved applied: "+strings.Join(unfinished, ", "),
			"review its durable state with bootwright status")
	}
	return transition{noop: true, verb: reconciliation.Apply, operation: operation, plan: frozen, states: states}, nil
}

// unfinishedBlocks names each block of a frozen plan whose record reads
// anything but done, with the state it reads, in frozen order. A lost record
// reads as pending.
func unfinishedBlocks(frozen reconciliation.Plan, states map[string]reconciliation.BlockState) []string {
	unfinished := []string{}
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == reconciliation.BlockDone {
			continue
		}
		if state == "" {
			state = reconciliation.BlockPending
		}
		unfinished = append(unfinished, block.ID+" ("+string(state)+")")
	}
	return unfinished
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
// finished with. recovered names what the invocation completed before it
// settled, if anything.
func settled(identity ContextIdentity, decided transition, recovered string) *OperationResult {
	operation := decided.operation.ID
	if operation == "" {
		operation = "none"
	}
	return &OperationResult{
		Context:   identity,
		Verb:      string(decided.verb),
		Steps:     steps(decided.plan, decided.states),
		Blocks:    blockResults(decided.plan, decided.states),
		Settled:   true,
		Recovered: recovered,
		Receipt: Receipt{
			Operation: operation, Verb: string(decided.verb),
			State: string(reconciliation.OperationDone), Next: "none",
		},
	}
}

// refuseStageBoundary refuses before any effect when the selected stages admit
// no work. It names the stage that would unblock the operation, so a selection
// mistake is corrected rather than silently doing nothing. While any block is
// failed, the work is the retry the scheduler would choose, so a selection
// that admits none of the failed blocks refuses whichever one comes first.
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
		if _, _, ok := retryCandidate(plan, states, selection, nil); ok {
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

// freshApply is the one fresh-plan decision: apply registers the transition it
// returns and plan previews that same transition, so each refusal it makes is
// the one both verbs report before registration. It is pure, because a preview
// takes it too: it compiles the frozen input and plans, and binds, reserves and
// writes nothing.
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
	return transition{
		fresh: true, verb: reconciliation.Apply, plan: plan, binding: binding, selection: selection,
		basis: basis{revision: view.Identity().Revision, input: inputDigest(view)},
	}, nil
}

// supersedable reports whether a fresh removal may replace an incomplete
// operation. Any apply that has not completed qualifies, because what it owns
// is every block it started and that set is the same whether a block stopped
// at a boundary, failed, or lost its outcome to an interruption. An unproved
// block is not an exception to that: the removal proves it before it registers
// anything, and a block it cannot prove refuses the removal there. An
// incomplete removal is continued instead, and only a failed one is replaced,
// because a removal that lost an outcome is resolved by repeating itself.
// Replacement is the only road out of a repaired adapter, since a continuation
// is frozen to the automation its operation registered under.
func supersedable(operation operationstore.Operation) bool {
	if operation.Verb == reconciliation.Apply {
		return operation.State != reconciliation.OperationDone
	}
	return operation.State == reconciliation.OperationFailed
}

// mayHaveStartedNothing reports whether an operation's own state admits that it
// owns no effect. An apply registers before its first block starts, so one that
// stopped before starting any is still running or paused. A completed apply's
// blocks are all done, a failed or unknown apply holds the block that made it
// so or, lagging behind them, blocks that are all done, and a failed removal
// whose blocks are all done is finalized before any decision reaches here.
func mayHaveStartedNothing(operation operationstore.Operation) bool {
	return operation.Verb == reconciliation.Apply &&
		(operation.State == reconciliation.OperationRunning || operation.State == reconciliation.OperationPaused)
}

// supersede plans a fresh removal over what an incomplete operation still owns:
// the blocks an apply started, or the blocks a removal has not yet proved gone.
func (s Service) supersede(ctx context.Context, view View, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (transition, error) {
	if operation.Verb == reconciliation.Apply {
		// An empty set the operation's own state cannot explain is refused by
		// freshDestroy first, whatever else the records say.
		owned, startedNothing := reconciliation.OwnedSubset(frozen, states), mayHaveStartedNothing(operation)
		if len(owned.Blocks) != 0 || startedNothing {
			if err := refuseContradictions(ctx, store, operation, frozen, states, attempts); err != nil {
				return transition{}, err
			}
		}
		decided, err := s.freshDestroy(ctx, operation.Executable, operation.ID, firstBinding(operation.Bindings),
			operation.Bindings, owned, startedNothing)
		if err != nil {
			return transition{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
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
	decided, err := s.freshDestroy(ctx, operation.Executable, operation.Source, reopen, release,
		reconciliation.RemainingSubset(frozen, states), false)
	if err != nil {
		return transition{}, err
	}
	return plannedFrom(decided, operation, states, attempts), nil
}

// plannedFrom records the durable state a transition was planned from. The
// decision is read under the shared lock and the effects run under the
// exclusive one, so the transition re-proves this before it registers. It
// keeps the input a fresh apply recorded compiling its plan.
func plannedFrom(decided transition, operation operationstore.Operation, states map[string]reconciliation.BlockState, attempts map[string]int) transition {
	planned := recorded(operation, states, attempts)
	decided.basis.operation, decided.basis.state, decided.basis.record = planned.operation, planned.state, planned.record
	decided.basis.blocks, decided.basis.attempts = planned.blocks, planned.attempts
	return decided
}

// recorded is the basis an operation's records give, sharing nothing with
// them. Its bindings stay an empty set rather than none, because the record
// it is compared with decodes an empty set that way.
func recorded(operation operationstore.Operation, states map[string]reconciliation.BlockState, attempts map[string]int) basis {
	record := operation
	record.Bindings = slices.Clone(operation.Bindings)
	return basis{
		operation: operation.ID, state: operation.State, record: record,
		blocks: maps.Clone(states), attempts: maps.Clone(attempts),
	}
}

// blockRecords reads each frozen block's record: the state it proves and the
// attempts it counts. A block retried by another invocation that failed again
// leaves the state it found, so only its count shows the retry. A lost record
// reads as pending with no attempt.
func blockRecords(ctx context.Context, store OperationStore, id string, frozen reconciliation.Plan) (map[string]reconciliation.BlockState, map[string]int, error) {
	states := make(map[string]reconciliation.BlockState, len(frozen.Blocks))
	attempts := make(map[string]int, len(frozen.Blocks))
	for _, block := range frozen.Blocks {
		record, err := store.Block(ctx, id, block.ID)
		if err != nil {
			return nil, nil, err
		}
		states[block.ID], attempts[block.ID] = record.State, record.Attempts
	}
	return states, attempts, nil
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
//
// An apply that started no block owns nothing, so its removal carries no block:
// it registers, performs nothing and completes, which releases what the apply
// claimed and leaves the context at rest rather than holding an apply that only
// the exact input it froze could ever continue. startedNothing says the caller
// proved the operation is such an apply (mayHaveStartedNothing). Anywhere else
// an empty set means the operation contradicts its own block records, and a
// removal of nothing would release the material the effects still on the host
// need, so it refuses.
func (s Service) freshDestroy(ctx context.Context, frozenBy operationstore.Executable, source, reopen string, release []string, owned reconciliation.Plan, startedNothing bool) (transition, error) {
	if len(owned.Blocks) == 0 && !startedNothing {
		return transition{}, failure("lifecycle.state",
			"the operation this removal supersedes records no block it still owns",
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
	result := presentation(decided)
	result.Context = ContextIdentity{Name: name}
	result.Receipt = Receipt{Operation: "none", Verb: string(decided.verb), State: "preview", Next: string(decided.verb)}
	if !decided.fresh {
		result.Receipt.Operation = decided.operation.ID
		result.Receipt.Next = "continue-" + string(decided.verb)
	}
	return s.options.Presenter.PresentLifecyclePlan(ctx, result)
}

// presentation is the plan a transition shows: what apply and destroy present
// before they confirm, and what plan previews of the same transition.
func presentation(decided transition) PlanResult {
	result := planPreview(decided.plan, decided.states, decided.selection)
	result.Verb = string(decided.verb)
	result.Continuation = !decided.fresh
	return result
}

// execute binds the Secrets the plan consumes, then performs the operation
// inside one held transaction. Binding happens first because acquiring
// confidential material takes the same store lock the transaction holds. A
// fresh apply raises its running evidence and claims its operation directory
// before it binds, so nothing it binds, claims or reserves is ever held under
// evidence that lets the context be updated or deleted.
func (s Service) execute(ctx context.Context, name string, decided transition) (*OperationResult, error) {
	// A fresh transition reads the bindings the context holds before any of
	// its transactions, so what it collects once it registers existed before
	// it began: a binding issued since is never touched.
	var held []string
	if decided.fresh {
		held = s.held(ctx, name)
	}
	record := &registering{}
	if decided.fresh && decided.verb == reconciliation.Apply {
		protected, err := s.protect(ctx, name, decided, record)
		if err != nil {
			return nil, s.unregistered(ctx, name, decided, "", record, err)
		}
		decided = protected
	}
	binding, material, err := s.bind(ctx, name, decided)
	if err != nil {
		return nil, s.unregistered(ctx, name, decided, binding, record, err)
	}
	defer clearMaterial(material)
	var result *OperationResult
	var completion removalCompletion
	var unreleased error
	err = s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		result, err = s.run(ctx, tx, decided, binding, material, record)
		// The state a completed removal's record left is captured here, after
		// finish wrote it, because the pristine publication re-proves exactly
		// that once the releases are done. What it gives back is what its
		// transition releases and the binding this invocation reopened. A
		// removal whose record reads done owes both even once the invocation
		// is interrupted, so they run under the recording boundary.
		if completedRemoval(decided, result) {
			completion, unreleased = captureRemoval(recordingContext(ctx), s.store(tx), result.Receipt.Operation, joinBindings(slices.Clone(decided.release), binding))
		}
		return err
	})
	switch record.outcome {
	case notRegistered:
		return nil, s.unregistered(ctx, name, decided, binding, record, err)
	case possiblyRegistered:
		// The index may name the operation, so it may own this binding and its
		// running evidence for its whole lifetime: every later attempt reopens
		// the one and needs the other. Both stay, and nothing is collected.
		return result, err
	}
	collect := decided.fresh
	// A completed removal no longer needs the material its apply bound, even
	// when a log fault it latched makes the invocation report a failure. Its
	// receipt stays done whether or not everything it owned was given back.
	if completedRemoval(decided, result) {
		if unreleased == nil {
			unreleased = s.completeRemoval(recordingContext(ctx), name, completion)
		}
		if unreleased != nil {
			err, collect = withCause(err, incompleteRemoval(unreleased)), false
		}
	}
	if collect {
		s.collect(ctx, name, held, joinBindings(slices.Clone(decided.release), binding))
	}
	return result, err
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
		if frozen := frozenBinding(decided); frozen != "" {
			err = s.unreopenable(ctx, name, decided, frozen, err)
		}
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
// reservations, sequential block execution and the evidence projection. What
// its registration left is recorded in record, whatever run returns.
func (s Service) run(ctx context.Context, tx Transaction, decided transition, binding string, material map[string]secrets.Material, record *registering) (*OperationResult, error) {
	store := s.store(tx)
	// A removal proves the outcome of every effect it would take back, and then
	// that none of it is still in use, before it registers. Proving either one
	// per effect instead would not do: a removal takes dependents first, so it
	// would take back the settled leaves and then stop at the running machine,
	// leaving a context that can only continue the destroy it should never have
	// started. Both proofs share one approved bundle, exactly as the operation's
	// own effects share one once it has registered.
	if decided.fresh && decided.verb == reconciliation.Destroy {
		proving, err := approvedBundle(ctx, tx)
		if err != nil {
			return nil, err
		}
		if err := s.proveRemovable(ctx, tx, store, proving, decided, material); err != nil {
			return nil, err
		}
		if err := s.proveQuiescent(ctx, proving, decided.plan, material); err != nil {
			return nil, err
		}
	}
	operation, plan, log, err := s.register(ctx, tx, store, decided, binding, record)
	if err != nil {
		return nil, err
	}
	result := &OperationResult{Context: tx.Identity(), Verb: string(operation.Verb), Steps: steps(plan, nil)}
	// A log fault requests cancellation of the work in flight, never of the
	// records that settle it, which are written under the recording boundary.
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	logging := newLogBoundary(store, operation.ID, cancel)
	// A continuation restored its operation log as it registered; a fresh
	// operation opens its own, which exists only once it is registered.
	if log == nil {
		if log, err = logging.open(ctx, operationstore.OperationLogPath(operation.ID)); err != nil {
			return result, err
		}
	}
	defer logging.close(ctx, log)
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
	boundary, cause := s.converge(work, tx, store, approved, logging, operation, plan, states, material, decided.selection)
	logging.close(ctx, log)
	final, terminal := s.finish(recordingContext(ctx), tx, store, operation, plan, states, boundary, logging.faulted(), result)
	return final, withCause(logging.err(), withCause(cause, terminal))
}

// Each gate is one step, and the blocks it covers are that step's sub-steps, so
// a removal of any size settles each proof as one row and the rows that follow
// are the plan's effects alone.
const (
	quiescenceCheck            = "quiescence"
	quiescenceCheckDescription = "prove nothing this removal takes back is still in use"
	resolutionCheck            = "resolution"
	resolutionCheckDescription = "prove the outcome of every effect this removal takes back"
)

// proveRemovable re-proves, under the exclusive lock, the operation this
// removal replaces, and resolves every effect of it whose outcome is still
// unproved. Both happen before the removal registers anything: an unproved
// effect admits no removal, because nothing says what it owns, and a resolution
// is read-only, so a removal that cannot prove one leaves the context exactly
// as it found it. Resolving needs the replaced operation's logging boundary,
// and a log fault recorded there blocks the removal until it is restored, so
// either one restores that boundary first.
func (s Service) proveRemovable(ctx context.Context, tx Transaction, store OperationStore, approved bundle, decided transition, material map[string]secrets.Material) error {
	if decided.basis.operation == "" {
		return nil
	}
	replaced, frozen, states, err := s.verifyBasis(ctx, store, decided.basis, func(basis) error {
		return failure("lifecycle.state",
			"the operation this removal was planned from is no longer the one the context holds",
			"repeat the removal to plan it from the operation the context holds now")
	})
	if err != nil {
		return err
	}
	unproved := unprovedBlocks(frozen, states)
	if len(unproved) == 0 && !replaced.LogFault {
		return nil
	}
	replaced, log, err := restore(ctx, store, replaced)
	if err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	logging := newLogBoundary(store, replaced.ID, cancel)
	defer logging.close(ctx, log)
	if len(unproved) == 0 {
		return nil
	}
	// A failed apply may hold a running block in place of the failed one it was
	// retrying, and a resolution may prove that block done. The apply first
	// takes the state its blocks give it, as a retry first records it running,
	// so a removal stopped before it records what it resolved never leaves a
	// failed apply that no block accounts for.
	if replaced.Verb == reconciliation.Apply && replaced.State == reconciliation.OperationFailed {
		if err := s.recordFromBlocks(ctx, tx, store, replaced, frozen, states); err != nil {
			return err
		}
	}
	observed := s
	reporter := &resolutionProgress{report: s.report, declared: len(unproved)}
	observed.options.Progress = reporter
	cause := observed.observe(work, tx, store, approved, logging, replaced, frozen, states, material)
	logging.close(ctx, log)
	if err := s.recordFromBlocks(recordingContext(ctx), tx, store, replaced, frozen, states); err != nil {
		return err
	}
	// A log fault refuses the removal even when every observation proved its
	// block, because the removal's own effects would follow.
	remaining, fault := unprovedBlocks(frozen, states), logging.err()
	if len(remaining) == 0 && fault == nil {
		s.reportResolution(ctx, ProgressEvent{Status: "ok"})
		return nil
	}
	s.reportResolution(ctx, ProgressEvent{Status: "unknown"})
	var unresolved error
	if len(remaining) != 0 {
		unresolved = failure("lifecycle.unknown",
			"this removal cannot prove what these effects left behind, so it registered nothing: "+strings.Join(remaining, ", "),
			"do what the diagnostic of each reports, then repeat bootwright destroy")
	}
	return withCause(fault, withCause(cause, unresolved))
}

// verifyBasis proves the context still holds exactly the durable state a
// transition was planned from, its operation's whole record, frozen plan and
// block records included, and otherwise returns what refuse makes of the
// state it holds instead. Anything else means another invocation advanced the
// context between the decision and this transaction, so the plan waiting to
// register may no longer describe what the context owns. A context that held
// no operation must still hold none.
func (s Service) verifyBasis(ctx context.Context, store OperationStore, planned basis, refuse func(current basis) error) (operationstore.Operation, reconciliation.Plan, map[string]reconciliation.BlockState, error) {
	fail := func(err error) (operationstore.Operation, reconciliation.Plan, map[string]reconciliation.BlockState, error) {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	index, err := store.Index(ctx)
	if err != nil {
		return fail(err)
	}
	current := basis{operation: index.Current}
	if current.operation == "" {
		if planned.operation != "" {
			return fail(refuse(current))
		}
		return operationstore.Operation{}, reconciliation.Plan{}, nil, nil
	}
	operation, err := store.ReadOperation(ctx, current.operation)
	if err != nil {
		return fail(err)
	}
	current.state, current.record = operation.State, operation
	if current.operation != planned.operation || current.state != planned.state {
		return fail(refuse(current))
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return fail(err)
	}
	states, attempts, err := blockRecords(ctx, store, operation.ID, frozen)
	if err != nil {
		return fail(err)
	}
	current.blocks, current.attempts = states, attempts
	if _, moved := recordsMoved(planned, current); moved {
		return fail(refuse(current))
	}
	return operation, frozen, states, nil
}

// recordsMoved compares the operation records two bases hold, in the order a
// refusal names them: the operation and its state, each block's state, each
// block's attempt count, the frozen plan through the digest the operation's
// record carries, and then the rest of that record. It returns the words a
// refusal adds for the first difference, which are none for the operation or
// its state, since the refusal names both. Every read of a frozen plan is held
// to that digest, so a different record implies any different plan; the
// digest is compared before the rest of the record only so that the refusal
// names the plan.
func recordsMoved(planned, current basis) (string, bool) {
	switch {
	case current.operation != planned.operation || current.state != planned.state:
		return "", true
	case !maps.Equal(current.blocks, planned.blocks):
		return " with different block states", true
	case !maps.Equal(current.attempts, planned.attempts):
		return " with block attempts recorded since it was read", true
	case current.record.PlanDigest != planned.record.PlanDigest:
		return " with a different frozen plan", true
	case !reflect.DeepEqual(current.record, planned.record):
		return " with a different operation record", true
	}
	return "", false
}

// contextChanged refuses a continuation or a fresh apply whose basis moved
// after it was presented and confirmed. It names what the command planned from
// and what the context holds now, because repeating the command decides again
// from the latter, which is the only plan it may register. The operation
// records are compared first, and the input and evidence only once they agree.
func contextChanged(verb reconciliation.Verb, planned, current basis) error {
	message := "the context changed after this command read it: it was planned from " + planned.describe() +
		", and the context now holds " + current.describe()
	records, moved := recordsMoved(planned, current)
	switch {
	case moved:
		message += records
	case current.revision != planned.revision:
		message += " at input revision " + current.revision + " rather than " + planned.revision
	case current.input != planned.input:
		message += " with different input content"
	case !bytes.Equal(current.evidence, planned.evidence):
		message += " with different mutation evidence"
	case !slices.Equal(current.claims, planned.claims):
		message += " with an operation claimed since it was read"
	}
	return failure("lifecycle.state", message, "repeat bootwright "+string(verb)+" to plan from what the context holds now")
}

// recordFromBlocks publishes the state the replaced operation's blocks give it.
// After the resolutions it records what they proved, so its durable state
// matches its blocks whether or not this removal goes on to register. A later
// invocation then takes the ordinary road instead of observing the same
// effects again.
func (s Service) recordFromBlocks(ctx context.Context, tx Transaction, store OperationStore, replaced operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	ordered := make([]reconciliation.BlockState, 0, len(frozen.Blocks))
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		ordered = append(ordered, state)
	}
	next, err := reconciliation.NextOperationState(ordered, false)
	if err != nil {
		return err
	}
	// Read again, because a log fault the resolutions latched rewrote the
	// record, and replacing it from the earlier read would clear the fault.
	current, err := store.ReadOperation(ctx, replaced.ID)
	if err != nil {
		return err
	}
	if next == current.State {
		return nil
	}
	current.State = next
	if err := store.UpdateOperation(ctx, current); err != nil {
		return err
	}
	return s.project(ctx, tx, current.Verb, next)
}

// unprovedBlocks names every block of a plan whose effect has no proved
// outcome, in frozen order.
func unprovedBlocks(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []string {
	var found []string
	for _, block := range plan.Blocks {
		if unproved(states[block.ID]) {
			found = append(found, block.ID)
		}
	}
	return found
}

// resolutionProgress reports the resolutions a removal performs as one check
// row whose sub-steps are the blocks it proves, which is how a proof that
// precedes every effect reports. The engine observes blocks concurrently, so
// what it counts is guarded.
type resolutionProgress struct {
	report   func(context.Context, ProgressEvent)
	declared int
	mutex    sync.Mutex
	settled  map[string]bool
}

func (p *resolutionProgress) ReportProgress(ctx context.Context, event ProgressEvent) {
	if event.Block == "" {
		return
	}
	p.mutex.Lock()
	if event.Status != "running" {
		if p.settled == nil {
			p.settled = map[string]bool{}
		}
		p.settled[event.Block] = true
	}
	completed := len(p.settled)
	p.mutex.Unlock()
	p.report(ctx, ProgressEvent{
		Phase: CheckPhase, Block: resolutionCheck, Description: resolutionCheckDescription,
		Group: event.Block, Detail: event.Description, Status: "running",
		Completed: completed, Declared: p.declared,
	})
}

func (p *resolutionProgress) ReportLogLocation(context.Context, string) {}

// reportResolution settles the resolution proof as one row. It runs before the
// operation exists, so it belongs to no block and counts against no frozen
// total.
func (s Service) reportResolution(ctx context.Context, event ProgressEvent) {
	event.Phase, event.Block, event.Description = CheckPhase, resolutionCheck, resolutionCheckDescription
	s.report(ctx, event)
}

// proveQuiescent observes every block a removal would take back and refuses
// while any of it is still in use. Every block is probed rather than the first
// live one alone, so an operator is told everything to stop instead of
// discovering the next obstacle each time they repeat the command. A probe
// that cannot read its target reports live, because an environment that cannot
// prove it is idle is never assumed to be.
func (s Service) proveQuiescent(ctx context.Context, approved bundle, plan reconciliation.Plan, material map[string]secrets.Material) error {
	var live, stops []string
	err := s.guard.WithPython(ctx, approved.area, approved.requirement, func(launch prerequisites.PythonLaunch, _ func() error) error {
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

// register publishes the operation this transition runs, or re-opens the one
// it continues, once the durable state it was planned from is proved unchanged,
// and records in record what its registration left. A fresh removal proved
// that state before it resolved anything, in this same transaction, and its
// resolutions are what may have moved it since; it raises its running evidence
// immediately before it registers, because the registration moves the index
// and no other invocation can restore that. A fresh apply raised its evidence
// and claimed its directory in a transaction of its own, so it re-proves both
// here and registers into that directory. Nothing is projected after the
// registration: the evidence already protects the operation it names.
func (s Service) register(ctx context.Context, tx Transaction, store OperationStore, decided transition, binding string, record *registering) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
	fail := func(err error) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	if !decided.fresh {
		operation, plan, log, err := s.resume(ctx, tx, store, decided, record)
		if err != nil {
			return fail(err)
		}
		record.outcome = registered
		return operation, plan, log, nil
	}
	identity := decided.basis.claimed
	if decided.verb == reconciliation.Apply {
		if err := s.reprove(ctx, tx, store, decided); err != nil {
			return fail(err)
		}
	} else {
		if err := record.survey(ctx, store); err != nil {
			return fail(err)
		}
		allocated, err := s.allocate(record)
		if err != nil {
			return fail(err)
		}
		identity = allocated
	}
	closure, err := executionClosure(tx.Controller())
	if err != nil {
		return fail(err)
	}
	if err := s.reserve(ctx, tx, decided); err != nil {
		return fail(err)
	}
	digest, err := decided.plan.Digest()
	if err != nil {
		return fail(err)
	}
	stamp := s.options.Clock.Now().UTC().Truncate(1e9).Format("2006-01-02T15:04:05Z07:00")
	bindings := []string{}
	if binding != "" {
		bindings = append(bindings, binding)
	}
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: identity, Verb: decided.verb,
		Context: tx.Identity().Name, Revision: tx.Identity().Revision,
		InputDigest: inputDigest(tx), PlanDigest: digest, AutomationDigest: s.automation.CatalogDigest(),
		Executable: operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit},
		Closure:    &closure,
		Source:     decided.source, Bindings: bindings, State: reconciliation.OperationRunning,
		Created: stamp, Updated: stamp,
	}
	if decided.verb == reconciliation.Destroy {
		if err := s.raise(ctx, tx, reconciliation.Destroy, record); err != nil {
			return fail(err)
		}
	}
	if err := s.publish(ctx, store, operation, decided.plan, record); err != nil {
		return fail(atTheBound(ctx, tx, store, decided.verb, err))
	}
	return operation, decided.plan, nil, nil
}

// resume re-opens the operation a continuation continues once everything it
// depends on is re-proved. It restores the operation's logging boundary and
// raises the running evidence before it marks the operation running, so a
// restoration that fails leaves the operation as it was and no block starts
// under evidence that does not protect it. It returns the operation log it
// reopened.
func (s Service) resume(ctx context.Context, tx Transaction, store OperationStore, decided transition, record *registering) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
	fail := func(err error) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	changed := func(current basis) error { return contextChanged(decided.verb, decided.basis, current) }
	operation, plan, _, err := s.verifyBasis(ctx, store, decided.basis, changed)
	if err != nil {
		return fail(err)
	}
	if err := s.verifyContinuation(ctx, tx, operation); err != nil {
		return fail(err)
	}
	operation, log, err := restore(ctx, store, operation)
	if err != nil {
		return fail(err)
	}
	err = record.survey(ctx, store)
	if err == nil {
		err = s.raise(ctx, tx, operation.Verb, record)
	}
	if err == nil && operation.State != reconciliation.OperationRunning {
		operation.State = reconciliation.OperationRunning
		err = store.UpdateOperation(ctx, operation)
	}
	if err != nil {
		_ = log.Close(recordingContext(ctx))
		return fail(err)
	}
	return operation, plan, log, nil
}

// reprove holds a fresh apply's registering transaction to what its
// protecting transaction left: the same basis and input, the running evidence
// it raised, and exactly the operation directories it listed after its own
// claim. Evidence bytes are no token, since a release that lowered them and a
// later claim that raised them again leave the same bytes, so the directories
// are proved too. A fresh apply is then where a context claims its controller
// host. Binding precedes every reservation and effect, so an operation never
// leaves work behind on a host the context is not recorded against.
func (s Service) reprove(ctx context.Context, tx Transaction, store OperationStore, decided transition) error {
	previous, err := s.verifyFreshApply(ctx, tx, store, decided)
	if err != nil {
		return err
	}
	claimed, err := store.Claimed(ctx)
	if err != nil {
		return err
	}
	current := decided.basis
	current.evidence, current.claims = tx.Evidence(), claimed
	if !bytes.Equal(current.evidence, decided.basis.evidence) || !slices.Equal(current.claims, decided.basis.claims) {
		return contextChanged(decided.verb, decided.basis, current)
	}
	// A completed removal that latched a log fault blocks the next apply
	// until its boundary is restored, as it would block its own work.
	if previous.LogFault {
		_, log, err := restore(ctx, store, previous)
		if err != nil {
			return err
		}
		_ = log.Close(recordingContext(ctx))
	}
	return s.establishBinding(ctx, tx, decided.binding.controller)
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
	registered := registeredWith(operation.Executable)
	if operation.Closure == nil && tx.Controller().SetupRuns {
		return failure("lifecycle.state", "this operation was registered by an earlier build that froze no Python and Ansible closure, and "+
			registered+" cannot read this host's controller directory, which now keeps setup runs", earlierBuildGone(tx, operation))
	}
	if operation.AutomationDigest != s.automation.CatalogDigest() {
		remediation := "install " + registered + ", which registered this operation, and run bootwright setup"
		if supersedable(operation) {
			remediation = "destroy what this operation owns under this executable, or install " + registered + ", which registered it"
		}
		return failure("lifecycle.state", "this executable's automation differs from the one this operation froze", remediation)
	}
	if operation.Closure == nil {
		remediation := "continue it with " + registered + ", which registered it"
		if supersedable(operation) {
			remediation = "destroy what this operation owns under this executable, or " + remediation
		}
		return failure("lifecycle.state", "this operation was registered by an earlier build that froze no Python and Ansible closure", remediation)
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	if err := verifyHostBinding(tx.Controller(), identity, host); err != nil {
		return err
	}
	current, err := executionClosure(tx.Controller())
	if err != nil {
		return err
	}
	if current != *operation.Closure {
		frozen := *operation.Closure
		remediation := "restore the execution bundle of Python " + frozen.Python + " and ansible-core " + frozen.Ansible +
			" that " + registered + " registered this operation with"
		if supersedable(operation) {
			remediation = "destroy what this operation owns under the approved bundle, or " + remediation
		}
		return failure("lifecycle.state", "the approved execution bundle holds another Python and Ansible closure (Python "+
			current.Python+", ansible-core "+current.Ansible+") than the one this operation registered with", remediation)
	}
	return nil
}

// earlierBuildGone is the remedy for an operation whose registering build
// froze no closure once setup kept a run on this host. Every such build
// predates setup runs and refuses a controller directory that keeps them, so
// no remedy can name it: a removal under this executable supersedes the
// operation where one may, and otherwise only deleting the context remains.
func earlierBuildGone(view View, operation operationstore.Operation) string {
	if supersedable(operation) {
		return "destroy what this operation owns under this executable"
	}
	return deletionExit(view)
}

// executionClosure is the Python and Ansible closure of the execution bundle
// the retained setup approves, which every effect of an operation runs in.
// The native packages setup installs beside it are the host's: the package
// manager may update them, so they are no part of it.
func executionClosure(view prerequisites.StorageView) (operationstore.Closure, error) {
	definition := view.State.Receipt.Definition
	if definition == nil || definition.Bootstrap == nil {
		return operationstore.Closure{}, failure("controller.state", "the retained controller setup has no execution definition", "run bootwright setup")
	}
	digest, err := prerequisites.ClosureDigest(*definition.Bootstrap)
	if err != nil {
		return operationstore.Closure{}, err
	}
	return operationstore.Closure{Digest: digest, Python: definition.Bootstrap.PythonVersion, Ansible: definition.Bootstrap.AnsibleVersion}, nil
}

// registeredWith names the build an operation registered under, as the
// version command spells it, or the record itself when it names none.
func registeredWith(executable operationstore.Executable) string {
	if identity := executableIdentity(executable); identity != "" {
		return "bootwright " + identity
	}
	return "the executable its operation.json records"
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
