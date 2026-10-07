package lifecycle

import (
	"bytes"
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// finalization marks an operation whose block records already prove it
// complete while its record, evidence, reservations or Secret bindings do not
// yet say so, which is what an invocation interrupted between its last outcome
// and its last write leaves. A running, unknown or failed operation is
// finalized only by its own verb, because the other verb over it decides for
// itself: a destroy supersedes an incomplete apply, removing every block it
// started, and an apply refuses an incomplete destroy. An unknown one whose
// blocks are all done has a record that lags behind them, as a removal stopped
// after its resolution proved an unknown apply's block and before it recorded
// the apply leaves it, so its blocks prove completion exactly as a running
// one's do. A failed removal whose blocks are all done lags the same way: a
// removal superseding it records nothing of it before it resolves the running
// block a failed retry start left, so one stopped after that resolution leaves
// it failed beside done blocks. A failed apply whose blocks are all done lags
// as well, as a removal an executable before 4513e123 stopped after resolving
// the running block of a failed retry start left it, because that executable
// did not first record the apply in the state its blocks gave it. A completed
// one is finalized under either verb, since only its own bookkeeping is left.
// Records that hold a block that is not done prove no completion, so they are
// never finalized. Anything else returns no mark and keeps its decision.
func finalization(ctx context.Context, view View, store OperationStore, verb reconciliation.Verb, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (transition, error) {
	if pendingRemains(frozen, states) {
		return transition{}, nil
	}
	switch operation.State {
	case reconciliation.OperationRunning, reconciliation.OperationUnknown, reconciliation.OperationFailed:
		if operation.Verb != verb {
			return transition{}, nil
		}
	case reconciliation.OperationDone:
		finished, err := finalized(view, operation)
		if err != nil || finished {
			return transition{}, err
		}
	default:
		return transition{}, nil
	}
	decided := transition{finalize: true, verb: verb, operation: operation, plan: frozen, states: states}
	if operation.Verb == reconciliation.Destroy {
		release, err := releaseSet(ctx, store, operation)
		if err != nil {
			return transition{}, err
		}
		decided.release = release
	}
	return plannedFrom(decided, operation, states, attempts), nil
}

// afterFinalization is the decision a verb takes once the finalization its
// first decision marked is done, taken without doing it. That finalization
// rewrites only the operation record's state, to the one its block records
// give; no decision after the mark reads the evidence, reservations or Secret
// bindings it also completes.
func (s Service) afterFinalization(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.StageSelection, marked transition) (transition, error) {
	next, err := finalState(marked.plan, marked.states)
	if err != nil {
		return transition{}, err
	}
	operation := marked.operation
	operation.State = next
	return s.decideOver(ctx, view, s.store(view), verb, selection, operation, marked.plan, marked.states, marked.basis.attempts)
}

// finalState is the state a finalization records: the one the operation's
// block records give.
func finalState(frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (reconciliation.OperationState, error) {
	return reconciliation.NextOperationState(orderedStates(frozen, states), false)
}

// finalized reports whether a completed operation's finalization is complete:
// its evidence is its projection and a completed removal leaves this context
// no reservation. A removal publishes pristine evidence only once its Secret
// bindings are released, so its pristine evidence proves those released too.
func finalized(view View, operation operationstore.Operation) (bool, error) {
	projected, err := projection(operation.Verb, reconciliation.OperationDone)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(view.Evidence(), projected) {
		return false, nil
	}
	return operation.Verb != reconciliation.Destroy || !holdsReservation(view), nil
}

func holdsReservation(view View) bool {
	name := view.Identity().Name
	return slices.ContainsFunc(view.Controller().State.Reservations, func(reservation prerequisites.HostReservation) bool {
		return reservation.Context == name
	})
}

// releaseSet is every Secret binding a completed removal gives back: the one it
// reopened, which its record names, and each binding of the apply it removed.
func releaseSet(ctx context.Context, store OperationStore, operation operationstore.Operation) ([]string, error) {
	release := joinBindings(nil, operation.Bindings...)
	if operation.Source == "" {
		return release, nil
	}
	applied, err := store.ReadOperation(ctx, operation.Source)
	if err != nil {
		return nil, err
	}
	return joinBindings(release, applied.Bindings...), nil
}

func joinBindings(set []string, bindings ...string) []string {
	for _, binding := range bindings {
		if binding != "" && !slices.Contains(set, binding) {
			set = append(set, binding)
		}
	}
	return set
}

// finalizeFirst completes the finalization a decision marked and then decides
// again from what the context holds once it is done, so the invocation goes on
// exactly as it would over an operation that had finalized on its own. A
// finalization that leaves another behind refuses rather than repeating.
func (s Service) finalizeFirst(ctx context.Context, name string, verb reconciliation.Verb, selection reconciliation.StageSelection, marked transition) (transition, ContextIdentity, error) {
	if err := s.finalize(ctx, name, marked); err != nil {
		return transition{}, ContextIdentity{}, err
	}
	decided, identity, err := s.decideShared(ctx, name, verb, selection)
	if err != nil {
		return transition{}, ContextIdentity{}, err
	}
	if decided.finalize {
		return transition{}, ContextIdentity{}, failure("lifecycle.state",
			"the operation's finalization did not complete",
			"repeat "+contextCommand(name, string(verb)))
	}
	return decided, identity, nil
}

// finalize performs, under the exclusive lock and once the state it was
// decided from is re-proved, only what the block records already prove: the
// operation's completed record, the releases a completed removal owes and the
// operation's projection. It performs no effect and starts no block, so it
// needs no authorization, presentation or confirmation. A removal's pristine
// publication lowers the evidence every fresh apply still in flight raised, so
// once it lands the bindings the context held before the finalization began
// that no operation names are released too.
func (s Service) finalize(ctx context.Context, name string, decided transition) error {
	var held []string
	if decided.operation.Verb == reconciliation.Destroy {
		held = s.held(ctx, name)
	}
	var completion removalCompletion
	err := s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		changed := func(current basis) error { return contextChanged(decided.verb, decided.basis, current) }
		operation, frozen, states, err := s.verifyBasis(ctx, store, decided.basis, changed)
		if err != nil {
			return err
		}
		next, err := finalState(frozen, states)
		if err != nil {
			return err
		}
		// The record verifyBasis read under this lock is the one the decision
		// read, log fault included, so rewriting it changes only its state.
		if operation.State != next {
			operation.State = next
			if err := store.UpdateOperation(ctx, operation); err != nil {
				return err
			}
		}
		if operation.Verb == reconciliation.Apply {
			return s.project(ctx, tx, operation.Verb, next)
		}
		// The removal's record reads done from here, so what it still owes is
		// given back under the recording boundary, interrupted or not: the
		// produced material first, then the reservations.
		recording := recordingContext(ctx)
		if err := s.withdraw(recording, tx); err != nil {
			return err
		}
		if err := releaseHeld(recording, tx); err != nil {
			return err
		}
		completion, err = captureRemoval(recording, store, operation.ID, decided.release)
		return err
	})
	if err != nil || decided.operation.Verb == reconciliation.Apply {
		return err
	}
	if err := incompleteRemoval(name, s.completeRemoval(recordingContext(ctx), name, completion)); err != nil {
		return err
	}
	s.collect(ctx, name, held, decided.release)
	return nil
}

// removalCompletion is what a completed removal still owes once its record is
// final: the Secret bindings it gives back, and the state that record left,
// which the pristine publication re-proves after those releases.
type removalCompletion struct {
	basis   basis
	release []string
}

// captureRemoval reads the state a completed removal's record left, inside the
// transaction that wrote it.
func captureRemoval(ctx context.Context, store OperationStore, id string, release []string) (removalCompletion, error) {
	operation, err := store.ReadOperation(ctx, id)
	if err != nil {
		return removalCompletion{}, err
	}
	frozen, err := store.ReadPlan(ctx, id)
	if err != nil {
		return removalCompletion{}, err
	}
	states, attempts, err := blockRecords(ctx, store, id, frozen)
	if err != nil {
		return removalCompletion{}, err
	}
	return removalCompletion{basis: recorded(operation, states, attempts), release: slices.Clone(release)}, nil
}

// completeRemoval gives back what a completed removal owned and only then
// publishes pristine evidence, so evidence that is not yet pristine is what
// marks the finalization unfinished for the next verb. The
// releases happen outside any transaction, because releasing a binding takes
// the store lock a transaction holds, and the publication re-proves that the
// removal is still the context's operation, in the state it was left in. Its
// callers pass the recording boundary, because a removal whose record reads
// done owes these even once its invocation is interrupted.
func (s Service) completeRemoval(ctx context.Context, name string, completion removalCompletion) error {
	completion.basis.context = name
	for _, binding := range completion.release {
		if _, err := s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: binding}); err != nil {
			return err
		}
	}
	return s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		changed := func(current basis) error { return contextChanged(reconciliation.Destroy, completion.basis, current) }
		if _, _, _, err := s.verifyBasis(ctx, store, completion.basis, changed); err != nil {
			return err
		}
		if err := releaseHeld(ctx, tx); err != nil {
			return err
		}
		if err := s.project(ctx, tx, reconciliation.Destroy, reconciliation.OperationDone); err != nil {
			return err
		}
		reclaim(ctx, tx, store)
		return nil
	})
}

// releaseHeld releases this context's reservations only while it holds one, so
// a removal whose reservations are already released writes nothing.
func releaseHeld(ctx context.Context, tx Transaction) error {
	if !holdsReservation(tx) {
		return nil
	}
	return tx.ReleaseReservations(ctx)
}

// incompleteRemoval reports a completed removal that could not give back
// everything it owned. Its record still says done, so repeating the destroy
// finalizes it instead of removing anything again.
func incompleteRemoval(contextName string, cause error) error {
	if cause == nil {
		return nil
	}
	return withCause(cause, failure("lifecycle.state",
		"the removal completed, but releasing what it owned is incomplete",
		"repeat "+contextCommand(contextName, string(reconciliation.Destroy))+" to finish it"))
}

// completedRemoval reports a removal whose run recorded it done.
func completedRemoval(decided transition, result *OperationResult) bool {
	return decided.verb == reconciliation.Destroy && result != nil && result.Receipt.State == string(reconciliation.OperationDone)
}

func orderedStates(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []reconciliation.BlockState {
	ordered := make([]reconciliation.BlockState, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		ordered = append(ordered, state)
	}
	return ordered
}
