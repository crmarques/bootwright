package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strconv"
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
	return s.mutate(ctx, reconciliation.Apply, request.ContextName, selection, request.Authorizations, nil, request.SkipConfirmation, request.SSH.Borrowed())
}

// Destroy removes what an apply recorded as owned. It accepts no stage
// selection: a removal covers exactly the effects that exist.
func (s Service) Destroy(ctx context.Context, request DestroyRequest) (*OperationResult, error) {
	return s.mutate(ctx, reconciliation.Destroy, request.ContextName, nil, request.Authorizations, request.Unregistered, request.SkipConfirmation, request.SSH.Borrowed())
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
	// stops is what a fresh removal's quiescence gate observes on the host:
	// the objects of its blocks whose capability probes quiescence, in plan
	// order.
	stops      []string
	sameBlocks sameBlocksProof
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
	// context names the context the decision read, so a refusal that finds
	// this basis moved names the command that decides again in that context.
	context string
}

// describe names the operation a basis holds and that operation's state, in
// the words a refusal reports them with.
func (b basis) describe() string {
	if b.operation == "" {
		return "no operation"
	}
	return "operation " + b.operation + " (" + string(b.state) + ")"
}

func (s Service) mutate(ctx context.Context, verb reconciliation.Verb, contextName string, selection reconciliation.StageSelection, authorizations, unregistered []string, skipConfirmation, borrowed bool) (*OperationResult, error) {
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
	if err := s.proveShared(ctx, name, decided); err != nil {
		return nil, err
	}
	if err := s.admitApply(ctx, name, decided); err != nil {
		return nil, err
	}
	// The plan is presented before authorization refuses, so a refusal names
	// steps the operator has just read; both precede the prompt and
	// registration.
	if err := s.present(ctx, name, decided); err != nil {
		return nil, err
	}
	if err := s.authorized(name, decided, authorizations, unregistered); err != nil {
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
	return s.execute(releasing(ctx, unregistered), name, decided)
}

func (s Service) authorized(name string, decided transition, authorizations, unregistered []string) error {
	if err := authorize(name, decided.verb, decided.selection, decided.plan, authorizations, unregistered); err != nil {
		return err
	}
	return s.acknowledgedReleases(name, decided.plan, unregistered)
}

func (s Service) admitApply(ctx context.Context, name string, decided transition) error {
	if decided.verb != reconciliation.Apply || s.options.ApplyAdmission == nil {
		return nil
	}
	return s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		return s.admitApplyView(ctx, view, decided)
	})
}

func (s Service) admitApplyView(ctx context.Context, view View, decided transition) error {
	if decided.verb != reconciliation.Apply || s.options.ApplyAdmission == nil {
		return nil
	}
	state, err := s.compile(ctx, view)
	if err != nil {
		return err
	}
	owned := map[string]ObjectOwnership{}
	starting := []reconciliation.Block{}
	for _, block := range decided.plan.Blocks {
		current := decided.states[block.ID]
		if current != "" && current != reconciliation.BlockPending {
			recordOwnership(owned, block, reconciliation.Apply, current)
		}
		if current != reconciliation.BlockDone && (unproved(current) || decided.selection.Selects(block.Stage)) {
			starting = append(starting, block)
		}
	}
	return s.options.ApplyAdmission(ctx, view.Identity().Name, state.Effective(), owned, starting)
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

// proveShared re-proves, under the shared lock and before the plan is
// presented, authorized or confirmed, what the transition's exclusive
// transaction proves again before it registers: a continuation or resolution
// its input, other-build blocks, closure, setup, host binding; a fresh removal
// its host. No plan is then presented for a transition that would refuse. A
// lost frozen binding names the only exit those records leave whatever else
// they hold, so it wins: a binding the keyring no longer lists has refused
// already, and one whose material the keyring cannot read, which only its
// reopen proves, is reported in place of the refusal here, as the reopen this
// transition would make before it registers reports it.
func (s Service) proveShared(ctx context.Context, name string, decided transition) error {
	removal := decided.fresh && decided.verb == reconciliation.Destroy
	if !removal && (decided.fresh || decided.operation.ID == "") {
		return nil
	}
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		if removal {
			return s.verifyRemovalHost(ctx, view)
		}
		return s.verifyContinuation(ctx, view, decided.operation, decided.sameBlocks, nil)
	})
	binding := frozenBinding(decided)
	if err == nil || binding == "" {
		return err
	}
	bound, reopened := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
	for _, item := range bound {
		item.Material.Clear()
	}
	if why, unreadable := unreadableMaterial(reopened); unreadable {
		return s.lostBinding(ctx, name, decided, decided.basis, binding, why)
	}
	return err
}

// authorize compares the tokens this invocation supplied with the tokens the
// whole frozen plan's blocks consume. A missing token refuses before
// registration, so an irreversible consequence is always acknowledged first; a
// token the plan does not consume refuses too, so a habitual authorization
// cannot pre-authorize a future destructive plan. Either refusal names the
// consuming steps by their place in the presented plan and the exact command
// that passes, which keeps the Machines the invocation names unregistered.
func authorize(contextName string, verb reconciliation.Verb, selection reconciliation.StageSelection, plan reconciliation.Plan, authorizations, unregistered []string) error {
	consumers := map[string][]string{}
	required := requiredTokens(plan)
	for index, block := range plan.Blocks {
		for _, token := range block.Consumes {
			consumers[token] = append(consumers[token], "step "+strconv.Itoa(index+1)+" ("+block.Description+")")
		}
	}
	var flags []string
	if len(selection) != 0 {
		flags = append(flags, "--stage", strings.Join(selection.Names(), ","))
	}
	flags = append(flags, authorizing(required)...)
	if len(unregistered) != 0 {
		flags = append(flags, "--unregistered", strings.Join(slices.Compact(slices.Sorted(slices.Values(unregistered))), ","))
	}
	passing := contextCommand(contextName, string(verb), flags...)
	supplied := slices.Compact(slices.Sorted(slices.Values(authorizations)))
	for _, token := range supplied {
		if !slices.Contains(required, token) {
			return failure("lifecycle.authorization",
				"this plan requires no "+token+" authorization",
				"repeat "+passing+" without --authorize "+token)
		}
	}
	for _, token := range required {
		if !slices.Contains(supplied, token) {
			return failure("lifecycle.authorization",
				"this plan has "+token+" consequences that are not authorized: "+strings.Join(consumers[token], ", "),
				"repeat it with the authorization: "+passing)
		}
	}
	return nil
}

// decide reads durable state and returns the one legal transition, planned
// from the context it read. Changed desired state never turns a continuation
// into a reconciliation. An operation whose finalization is due is marked
// before any other decision is taken.
func (s Service) decide(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.StageSelection) (transition, error) {
	decided, err := s.decideFrom(ctx, view, verb, selection)
	decided.basis.context = view.Identity().Name
	return decided, err
}

func (s Service) decideFrom(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.StageSelection) (transition, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if errors.Is(err, operationstore.ErrRetiredIndex) {
		return transition{}, operationstore.RetiredIndexRefusal(view.Identity().Name)
	}
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
	name := view.Identity().Name
	if operation.State != reconciliation.OperationDone {
		if operation.Verb != verb {
			return transition{}, s.refuseOver(ctx, view, operation, frozen, states)
		}
		if err := refuseUncontinuable(ctx, store, name, operation, frozen); err != nil {
			return transition{}, err
		}
		if verb == reconciliation.Apply {
			if err := refuseStageBoundary(name, frozen, states, selection); err != nil {
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
		decided.sameBlocks = s.proveSameBlocks(ctx, view, operation, frozen, states)
		return plannedFrom(decided, operation, states, attempts), nil
	}
	if operation.Verb == reconciliation.Apply {
		if verb == reconciliation.Apply {
			return s.settleApply(ctx, view, operation, frozen, states)
		}
		owned, err := completedOwnership(name, operation, frozen, states)
		if err != nil {
			return transition{}, err
		}
		decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.ID, firstBinding(operation.Bindings),
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

// refuseOver refuses a verb over an incomplete operation of the other verb,
// which only an apply over an incomplete removal reaches. It names that
// removal's continuation, or the fresh removal that replaces a failed one,
// only where the proof that command would make admits it: the continuation's
// re-proof, or the replacement's host proof. Otherwise it names the exit that
// proof's refusal names, followed by that refusal, so the remedy is never a
// command that refuses. A finalization runs no block and is held to neither.
func (s Service) refuseOver(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	const message = "must be continued before another operation"
	name := view.Identity().Name
	next := nextAction(operation, frozen, states)
	var proof error
	switch {
	case next == string(reconciliation.Destroy):
		proof = s.verifyRemovalHost(ctx, view)
	case pendingRemains(frozen, states):
		proof = s.verifyContinuation(ctx, view, operation, s.proveSameBlocks(ctx, view, operation, frozen, states), nil)
	}
	refused := diagnostics.Of(proof)
	if proof != nil && len(refused) == 0 {
		return proof
	}
	if len(refused) != 0 {
		return withCause(failure("lifecycle.state", "an incomplete "+string(operation.Verb)+" "+message, refused[0].Remediation), proof)
	}
	remediation := "continue it with "
	if next == string(reconciliation.Destroy) {
		remediation = "replace it with "
	}
	return failure("lifecycle.state", "an incomplete "+string(operation.Verb)+" "+message,
		remediation+continuationCommand(name, operation, frozen, states))
}

// completedOwnership is what a completed apply owns: its whole frozen plan,
// because an apply completes only once every block of it is done. A block whose
// record reads anything else contradicts that completion, and a lost record is
// such a block, since absence reads back as pending. A removal of the done rest
// would leave that block's effect in place and then release the binding it
// needs, so the removal refuses instead, naming every such block, before it
// binds, probes, registers or releases anything. An incomplete apply and a
// failed removal legitimately hold blocks that are not done and never come here.
func completedOwnership(contextName string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (reconciliation.Plan, error) {
	if unfinished := unfinishedBlocks(frozen, states); len(unfinished) != 0 {
		return reconciliation.Plan{}, failure("lifecycle.state",
			"the completed apply "+operation.ID+" records no block completion for these blocks, and a removal that skipped one would leave its effect in place: "+strings.Join(unfinished, ", "),
			reviewStatus(contextName))
	}
	return reconciliation.OwnedSubset(frozen, states), nil
}

// settleApply is an apply over a completed apply. A changed input refuses by
// naming what changed and the removal it needs. The unchanged input settles only
// while every block of the frozen plan is done, because the settled verb reports
// that completion: a block whose record reads anything else, a lost one included,
// contradicts it, and nothing then proves the input applied.
func (s Service) settleApply(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (transition, error) {
	name := view.Identity().Name
	if inputDrift(view, operation) != "" {
		return transition{}, s.changedInput(ctx, view, operation)
	}
	if unfinished := unfinishedBlocks(frozen, states); len(unfinished) != 0 {
		return transition{}, failure("lifecycle.state",
			"the completed apply "+operation.ID+" records no block completion for these blocks, so this input cannot be proved applied: "+strings.Join(unfinished, ", "),
			reviewStatus(name))
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

const (
	driftRevision = "revision"
	driftBytes    = "bytes"
)

func inputDrift(view View, operation operationstore.Operation) string {
	identity := view.Identity()
	switch {
	case operation.Context != identity.Name || operation.Revision != identity.Revision:
		return driftRevision
	case operation.InputDigest != inputDigest(view):
		return driftBytes
	}
	return ""
}

func (s Service) changedInput(ctx context.Context, view View, operation operationstore.Operation) error {
	const message = "the desired state changed after this apply completed"
	name := view.Identity().Name
	remedy := "take back what it owns with " + contextCommand(name, string(reconciliation.Destroy)) + ", then run " + contextCommand(name, string(reconciliation.Apply))
	named, ok := s.namedChange(ctx, view, operation)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ok {
		return failure("lifecycle.state", message, remedy)
	}
	return failure("lifecycle.state", message+": "+named, remedy+"; "+contextCommand(name, "diff")+" shows each change")
}

func (s Service) namedChange(ctx context.Context, view View, operation operationstore.Operation) (string, bool) {
	sources, err := view.Revision(ctx, operation.Revision)
	if err != nil || digestOf(sources) != operation.InputDigest {
		return "", false
	}
	before, err := s.compileSources(ctx, sources)
	if err != nil {
		return "", false
	}
	after, err := s.compile(ctx, view)
	if err != nil {
		return "", false
	}
	if operation.InputDigest == inputDigest(view) {
		return "its input revision changed, though its bytes repeat the ones this apply froze", true
	}
	changes := objectChanges(before.Effective(), after.Effective())
	if len(changes) == 0 {
		return "its bytes changed, but it declares the same objects", true
	}
	return describeChanges(changes), true
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

// refuseStageBoundary refuses before any effect exactly when the selected
// stages admit no work, as admission's first choice decides it. It names the
// exact apply that would unblock the operation, the selection widened by the
// stage it needs, so a selection mistake is corrected rather than silently
// doing nothing. That apply is authorized against the whole plan, so it
// carries every token the plan consumes and passes authorization as named.
// While any block is failed, the work is the retries the scheduler would
// start, so a selection that admits none of the failed blocks refuses whichever
// one comes first.
func refuseStageBoundary(contextName string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.StageSelection) error {
	kind, admitted := firstAdmission(plan, states, selection, nil, nil, nil)
	if len(admitted) != 0 {
		return nil
	}
	tokens := authorizing(requiredTokens(plan))
	if kind == StepRetry {
		failed, _ := plan.Block(heldBehind(plan, states, nil))
		return failure("lifecycle.stage",
			"the block this operation must retry is outside the selected stages",
			"repeat "+widenedApply(contextName, selection, failed.Stage, tokens))
	}
	ready := reconciliation.Ready(plan, states)
	if len(ready) == 0 {
		return failure("lifecycle.stage", "the selected stages have nothing to start",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply), tokens...)+" without --stage")
	}
	return failure("lifecycle.stage",
		"the selected stages have nothing to start",
		"repeat "+widenedApply(contextName, selection, ready[0].Stage, tokens))
}

// widenedApply is the apply whose selection adds the stage a refusal needs, in
// the canonical order --stage lists them, with the plan's authorizations.
func widenedApply(contextName string, selection reconciliation.StageSelection, needed reconciliation.Stage, tokens []string) string {
	stages := slices.Clone(selection)
	if !slices.Contains(stages, needed) {
		stages = append(stages, needed)
	}
	slices.SortStableFunc(stages, func(x, y reconciliation.Stage) int {
		return slices.Index(reconciliation.Stages(), x) - slices.Index(reconciliation.Stages(), y)
	})
	return contextCommand(contextName, string(reconciliation.Apply), append([]string{"--stage", strings.Join(stages.Names(), ",")}, tokens...)...)
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
	if err := refuseStageBoundary(view.Identity().Name, plan, nil, selection); err != nil {
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
// Replacement is the road out of a repair that changes what a block freezes,
// since a continuation keeps its frozen blocks.
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
	name := view.Identity().Name
	if operation.Verb == reconciliation.Apply {
		// An empty set the operation's own state cannot explain is refused by
		// freshDestroy first, whatever else the records say.
		owned, startedNothing := reconciliation.OwnedSubset(frozen, states), mayHaveStartedNothing(operation)
		if len(owned.Blocks) != 0 || startedNothing {
			if err := refuseContradictions(ctx, store, name, operation, frozen, states, attempts); err != nil {
				return transition{}, err
			}
		}
		decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.ID, firstBinding(operation.Bindings),
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
	decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.Source, reopen, release,
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
func blockRecords(ctx context.Context, store recordReader, id string, frozen reconciliation.Plan) (map[string]reconciliation.BlockState, map[string]int, error) {
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
func (s Service) freshDestroy(ctx context.Context, contextName string, frozenBy operationstore.Executable, source, reopen string, release []string, owned reconciliation.Plan, startedNothing bool) (transition, error) {
	if len(owned.Blocks) == 0 && !startedNothing {
		return transition{}, failure("lifecycle.state",
			"the operation this removal supersedes records no block it still owns",
			reviewStatus(contextName))
	}
	plan, err := s.removalOf(ctx, owned, frozenBy)
	if err != nil {
		return transition{}, err
	}
	return transition{
		fresh: true, verb: reconciliation.Destroy, plan: plan,
		source: source, reopen: reopen, release: release, stops: s.stops(plan),
	}, nil
}

// stops names the objects whose removal the quiescence gate refuses while
// they run, so the plan says what to stop before the operator confirms it.
func (s Service) stops(plan reconciliation.Plan) []string {
	var named []string
	boundary := s.onlineRemoval(plan)
	for _, block := range plan.Blocks {
		if boundary.machines[block.ID] || boundary.preStopMachines[block.ID] {
			continue
		}
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !ok {
			continue
		}
		if prober, probes := capability.(QuiescenceProber); probes && prober.ProbesQuiescence() && !slices.Contains(named, block.Object) {
			named = append(named, block.Object)
		}
	}
	return named
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
	return "remove it with " + registeredWith(executable)
}

func (s Service) present(ctx context.Context, name string, decided transition) error {
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
func (s Service) presentation(decided transition) PlanResult {
	result := planPreview(decided.plan, decided.states, decided.selection)
	result.Verb = string(decided.verb)
	result.Continuation = !decided.fresh
	// Ordinary removal keeps its pre-registration gate. An online removal's
	// later Machine boundary is re-proved on each continuation that needs it.
	if decided.fresh && decided.verb == reconciliation.Destroy {
		result.Stops = slices.Clone(decided.stops)
	}
	if decided.verb == reconciliation.Destroy {
		result.StopsAfterOnline = s.stopsAfterOnline(decided.plan, decided.states)
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
	if err := s.proveBindings(ctx, name, decided, material); err != nil {
		return nil, s.unregistered(ctx, name, decided, binding, record, err)
	}
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
			err, collect = withCause(err, incompleteRemoval(name, unreleased)), false
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

// run joins every adapter before releasing its execution scope and transaction.
func (s Service) run(ctx context.Context, tx Transaction, decided transition, binding string, material map[string]secrets.Material, record *registering) (*OperationResult, error) {
	if err := s.admitApplyView(ctx, tx, decided); err != nil {
		return nil, err
	}
	if s.options.BeginInvocation != nil {
		var release func()
		ctx, release = s.options.BeginInvocation(ctx)
		defer release()
	}
	store := s.store(tx)
	// A removal proves every effect's outcome and quiescence before registering.
	// Only Machines following opted-in online or pre-stop removal defer their stop
	// proof until those effects are durably absent. Unrelated capabilities keep
	// the all-or-nothing gate. The proofs, the registration and every attempt
	// share one located runtime, whose closure it freezes.
	if decided.fresh && decided.verb == reconciliation.Destroy {
		// The host is proved before anything is resolved, probed or
		// registered, so no removal touches a context from a host its
		// controller state does not name.
		if err := s.verifyRemovalHost(ctx, tx); err != nil {
			return nil, err
		}
	}
	located, err := s.approvedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if decided.fresh && decided.verb == reconciliation.Destroy {
		if err := s.proveRemovable(ctx, tx, store, located, decided, material); err != nil {
			return nil, err
		}
		boundary := s.onlineRemoval(decided.plan)
		skip := maps.Clone(boundary.machines)
		maps.Copy(skip, boundary.preStopMachines)
		if err := s.proveQuiescent(ctx, store, tx.Identity().Name, located, decided.plan, material, decided.source, skip); err != nil {
			return nil, err
		}
	}
	operation, plan, log, err := s.register(ctx, tx, store, decided, binding, record, located)
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
	boundary, cause := s.converge(work, tx, store, located, logging, operation, plan, states, material, decided.selection)
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
func (s Service) proveRemovable(ctx context.Context, tx Transaction, store OperationStore, approved approved, decided transition, material map[string]secrets.Material) error {
	if decided.basis.operation == "" {
		return nil
	}
	name := tx.Identity().Name
	replaced, frozen, states, err := s.verifyBasis(ctx, store, decided.basis, func(basis) error {
		return failure("lifecycle.state",
			"the operation this removal was planned from is no longer the one the context holds",
			"repeat "+contextCommand(name, string(reconciliation.Destroy))+" to plan it from the operation the context holds now")
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
	// An apply's block its own observation proves completed is resolved done,
	// with what it produced captured before any inverse runs (D123); any other
	// is resolved by this removal's own check (D119), which proves what it
	// takes back is this context's own, or absent, rather than that the apply's
	// frozen request was realized.
	removal := replaced.Verb == reconciliation.Apply
	cause := observed.observe(work, tx, store, approved, logging, replaced, frozen, states, material, removal, decided.plan)
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
			"do what the diagnostic of each reports, then repeat "+contextCommand(name, string(reconciliation.Destroy), authorizing(requiredTokens(decided.plan))...))
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
func (s Service) verifyBasis(ctx context.Context, store recordReader, planned basis, refuse func(current basis) error) (operationstore.Operation, reconciliation.Plan, map[string]reconciliation.BlockState, error) {
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
	return failure("lifecycle.state", message, "repeat "+contextCommand(planned.context, string(verb))+" to plan from what the context holds now")
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

// proveQuiescent observes the blocks assigned to one removal boundary and
// refuses while any is still in use. Every block is probed rather than the first
// live one alone, so an operator is told everything to stop instead of
// discovering the next obstacle each time they repeat the command. A probe
// that cannot read its target reports live, because an environment that cannot
// prove it is idle is never assumed to be.
func (s Service) proveQuiescent(ctx context.Context, store OperationStore, contextName string, approved approved, plan reconciliation.Plan, material map[string]secrets.Material, applyOperation string, skip map[string]bool) error {
	var live, stops, remedies []string
	generic := false
	blocks := slices.DeleteFunc(slices.Clone(plan.Blocks), func(block reconciliation.Block) bool { return skip[block.ID] })
	retired := s.onlineRemoval(plan).retired
	err := s.withPython(ctx, approved, func(launch prerequisites.PythonLaunch) error {
		for index, block := range blocks {
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
				Completed: index, Declared: len(blocks),
			})
			applyProved, err := applyDependencies(ctx, store, applyOperation, block.ID)
			if err != nil {
				return err
			}
			state, err := capability.Quiescent(ctx, Probe{
				ApplyOperation: applyOperation, ApplyProved: applyProved, Context: contextName, Block: block, Launch: launch, Material: material,
				RetiredWithHosts: retired[block.ID],
			})
			if err != nil {
				return err
			}
			if state.Settled() {
				continue
			}
			live = append(live, block.Kind+"/"+block.Object+" ("+state.Reason+")")
			if state.Remedy != "" {
				if !slices.Contains(remedies, state.Remedy) {
					remedies = append(remedies, state.Remedy)
				}
				continue
			}
			generic = true
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
	repeat := "then repeat " + contextCommand(contextName, string(reconciliation.Destroy), authorizing(requiredTokens(plan))...)
	remediation := "stop what is running, " + repeat
	if len(stops) != 0 {
		remediation = "stop it with " + strings.Join(stops, ", then ") + ", " + repeat
	}
	if len(remedies) != 0 {
		named := strings.Join(remedies, ", ")
		if generic {
			named += "; " + remediation
		}
		remediation = named
	}
	return failure("lifecycle.live", "this removal would take back state that is still in use: "+strings.Join(live, ", "), remediation)
}

// reportCheck streams one row of a read-only gate. It belongs to no effect
// block and counts against no frozen total, including at the online boundary.
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
func (s Service) register(ctx context.Context, tx Transaction, store OperationStore, decided transition, binding string, record *registering, located approved) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
	fail := func(err error) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	if !decided.fresh {
		operation, plan, log, err := s.resume(ctx, tx, store, decided, record, located)
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
	closure := located.closure
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
	if decided.verb == reconciliation.Destroy && decided.basis.record.Verb == reconciliation.Destroy &&
		decided.basis.operation != "" && decided.basis.operation != decided.source {
		operation.Replaces = decided.basis.operation
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
func (s Service) resume(ctx context.Context, tx Transaction, store OperationStore, decided transition, record *registering, located approved) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
	fail := func(err error) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	changed := func(current basis) error { return contextChanged(decided.verb, decided.basis, current) }
	operation, plan, _, err := s.verifyBasis(ctx, store, decided.basis, changed)
	if err != nil {
		return fail(err)
	}
	if err := s.verifyContinuation(ctx, tx, operation, decided.sameBlocks, &located); err != nil {
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
	if !setupComplete(tx.Controller()) {
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright setup")
	}
	return tx.Bind(ctx, machine, host)
}

// verifyContinuation re-proves everything a continuation depends on before it
// does work. Drift refuses; it never re-resolves to another implementation.
func (s Service) verifyContinuation(ctx context.Context, view View, operation operationstore.Operation, proof sameBlocksProof, located *approved) error {
	if err := s.recordedRefusal(view, operation, proof); err != nil {
		return err
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	if err := verifyHostBinding(view.Controller(), view.Identity(), host, unboundExit(view, operation), false); err != nil {
		return err
	}
	return s.closureRefusal(ctx, view, operation, located)
}

// verifyRemovalHost proves a fresh removal runs on the host the controller
// state records: setup completed there, it is that state's own host, and a
// binding the state records for the context names it. A context the state
// records no binding for is admitted once the host is proved, because no
// apply can bind a context that holds an incomplete operation, the removal is
// that context's exit, and the apply after it binds again (unboundExit).
func (s Service) verifyRemovalHost(ctx context.Context, view View) error {
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	return verifyHostBinding(view.Controller(), view.Identity(), host, "", true)
}

// recordedRefusal is the part of a continuation's re-proof that reads only
// what the context and this executable record: the input the operation froze,
// the closure an earlier build may have left unfrozen, and another build's proof
// of the same blocks. status reads it without probing the host.
func (s Service) recordedRefusal(view View, operation operationstore.Operation, proof sameBlocksProof) error {
	switch inputDrift(view, operation) {
	case driftRevision:
		return failure("lifecycle.state", "the context input changed after this operation registered", "restore the exact input revision this operation froze")
	case driftBytes:
		return failure("lifecycle.state", "the frozen input no longer matches this operation", "restore the exact input revision this operation froze")
	}
	if s.otherBuild(operation) && !proof.proved {
		if proof.refusal != nil {
			return proof.refusal
		}
		return failure("lifecycle.state", "this executable has not proved it freezes the blocks this operation froze", otherBuildRemediation(view, operation))
	}
	return nil
}

// closureRefusal refuses a continuation whose frozen closure, the runtime lock
// and interpreter ABI, differs from this build's; nil located is located here
// as the continuation's run locates it, rebuilding a missing runtime.
func (s Service) closureRefusal(ctx context.Context, view View, operation operationstore.Operation, located *approved) error {
	if located == nil {
		current, err := s.approvedRuntime(ctx)
		if err != nil {
			return err
		}
		located = &current
	}
	current := located.closure
	if current != *operation.Closure {
		frozen := *operation.Closure
		remediation := "continue it with " + registeredWith(operation.Executable) + ", whose runtime this host keeps (Python " + frozen.Python + ", ansible-core " + frozen.Ansible + ")"
		if supersedable(operation) {
			remediation += ", or take back what it owns with " + contextCommand(view.Identity().Name, string(reconciliation.Destroy))
		}
		return failure("lifecycle.state", "this build's controller runtime carries another Python and Ansible closure (Python "+
			current.Python+", ansible-core "+current.Ansible+") than the one this operation registered with", remediation)
	}
	return nil
}

// takeBackUnder is the removal that supersedes an operation its own build can
// no longer continue: a fresh destroy run under what this executable holds.
func takeBackUnder(contextName, under string) string {
	return "take back what this operation owns under " + under + " with " + contextCommand(contextName, string(reconciliation.Destroy))
}

// registeredWith names the build an operation registered under, as the
// version command spells it, or the record itself when it names none.
func registeredWith(executable operationstore.Executable) string {
	if identity := buildIdentity(executable); identity != "" {
		return "bootwright " + identity
	}
	return "the executable its operation.json records"
}

// verifyHostBinding proves this context is bound to the host the operation
// runs on and that its setup completed, before any local effect. unbound is
// the remedy when the controller state records no binding for the context,
// and admitUnbound admits that context instead, once the host is proved.
func verifyHostBinding(view prerequisites.StorageView, identity ContextIdentity, host controller.InstalledHostIdentity, unbound string, admitUnbound bool) error {
	if !view.Exists || !view.Prepared {
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright setup")
	}
	if !view.Initialized {
		if admitUnbound {
			return nil
		}
		return failure("controller.identity", "this context is not bound to a controller host", unbound)
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
	if admitUnbound {
		return nil
	}
	return failure("controller.identity", "this context is not bound to a controller host", unbound)
}

// unboundExit is the remedy of a continuation whose context the controller
// state records no binding for. Only a fresh apply binds, and an apply over an
// incomplete operation continues it or refuses, so no apply can bind here. The
// host is already proved the controller state's own: the removal that
// supersedes the operation takes back what it owns where one may, and the
// apply after it binds again. Otherwise only the binding's restoration or the
// context's deletion remains.
func unboundExit(view View, operation operationstore.Operation) string {
	const restore = "restore the controller state that recorded this context's binding from a matching backup"
	if supersedable(operation) {
		name := view.Identity().Name
		return "take back what this operation owns with " + contextCommand(name, string(reconciliation.Destroy)) +
			", after which " + contextCommand(name, string(reconciliation.Apply)) + " binds the context again, or " + restore
	}
	return restore + ", or " + deletionExit(view)
}

// inputDigest binds the operation to the exact frozen bytes it planned from,
// independently of the revision identity the registry records.
func inputDigest(view View) string {
	return digestOf(view.Inputs())
}

func digestOf(sources desiredstate.Sources) string {
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
