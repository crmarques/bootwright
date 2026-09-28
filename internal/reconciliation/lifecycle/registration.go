package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// registration is what a transition's registration left, as far as the
// invocation that attempted it can prove.
type registration int

const (
	// notRegistered is proved: nothing names the operation, so whatever the
	// invocation bound or raised for it is owned by nothing.
	notRegistered registration = iota
	// registered is proved: the index names the operation, or the operation a
	// continuation re-opened is running again.
	registered
	// possiblyRegistered is an index write that failed where it may have
	// landed, so the operation it wrote may own everything raised for it.
	possiblyRegistered
)

// registering is what one invocation's registration did as it went, so an
// outcome that provably registered nothing gives back exactly what it raised
// and nothing a newer invocation raised since.
type registering struct {
	outcome registration
	// raised reports that the running evidence is this invocation's to give
	// back: a running projection it attempted may have landed, whether or not
	// its publication reported success, or it claimed a directory under
	// running evidence it found, which makes every older claim refuse.
	raised bool
	// known are the operation directories read before anything was raised,
	// with the one this invocation claimed or registered. Nothing removes an
	// operation directory, so one beyond them is a newer invocation's claim,
	// and the evidence is then that invocation's.
	known []string
}

// survey reads the operation directories before anything is raised.
func (r *registering) survey(ctx context.Context, store OperationStore) error {
	claimed, err := store.Claimed(ctx)
	if err != nil {
		return err
	}
	r.known = claimed
	return nil
}

// newer reports whether a directory exists that this invocation neither read
// nor created.
func (r *registering) newer(claimed []string) bool {
	return slices.ContainsFunc(claimed, func(directory string) bool { return !slices.Contains(r.known, directory) })
}

// allocate picks an operation identity no directory holds, and counts it as
// this invocation's own before anything creates it, so a creation that fails
// part way is never mistaken for a newer claim.
func (s Service) allocate(record *registering) (string, error) {
	identity, err := reconciliation.AllocateOperationID(s.options.Entropy, func(candidate string) bool {
		return slices.Contains(record.known, candidate)
	})
	if err != nil {
		return "", err
	}
	record.known = append(record.known, identity)
	return identity, nil
}

func projection(verb reconciliation.Verb, state reconciliation.OperationState) ([]byte, error) {
	evidence, err := reconciliation.EvidenceFor(verb, state)
	if err != nil {
		return nil, err
	}
	return evidence.Bytes()
}

// raise publishes the running evidence of verb before the step it protects,
// unless the context already reads so, and records the attempt first.
func (s Service) raise(ctx context.Context, tx Transaction, verb reconciliation.Verb, record *registering) error {
	running, err := projection(verb, reconciliation.OperationRunning)
	if err != nil {
		return err
	}
	if bytes.Equal(tx.Evidence(), running) {
		return nil
	}
	record.raised = true
	return tx.PublishEvidence(ctx, running)
}

// settle publishes the evidence of verb in state unless the context already
// reads so.
func (s Service) settle(ctx context.Context, tx Transaction, verb reconciliation.Verb, state reconciliation.OperationState) error {
	data, err := projection(verb, state)
	if err != nil || bytes.Equal(tx.Evidence(), data) {
		return err
	}
	return tx.PublishEvidence(ctx, data)
}

// protect is a fresh apply's first transaction. Once the state and input it
// was planned from are re-proved, it claims the operation's directory and then
// raises the running evidence, before the apply binds a Secret, claims the
// controller host or reserves anything, so none of those is ever held under
// evidence that lets the context be updated or deleted. The claim comes first
// so that no raise, even one interrupted before it returns, ever lands without
// moving the claim count an older apply still in flight re-proves. A claim
// that fails may have created its directory, and that directory makes every
// older apply refuse, so the evidence is then this invocation's to give back
// unless a listing proves the directory absent. The transition it returns
// requires that evidence and that claim when it registers.
func (s Service) protect(ctx context.Context, name string, decided transition, record *registering) (transition, error) {
	protected := decided
	err := s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		if _, err := s.verifyFreshApply(ctx, tx, store, decided); err != nil {
			return err
		}
		if err := record.survey(ctx, store); err != nil {
			return err
		}
		identity, err := s.allocate(record)
		if err != nil {
			return err
		}
		if err := store.Claim(ctx, identity); err != nil {
			claimed, listed := store.Claimed(recordingContext(ctx))
			record.raised = listed != nil || slices.Contains(claimed, identity)
			return err
		}
		record.raised = true
		if err := s.raise(ctx, tx, reconciliation.Apply, record); err != nil {
			return err
		}
		claimed, err := store.Claimed(ctx)
		if err != nil {
			return err
		}
		protected.basis.evidence, protected.basis.claimed, protected.basis.claims = tx.Evidence(), identity, len(claimed)
		return nil
	})
	if err != nil {
		return decided, err
	}
	return protected, nil
}

// verifyFreshApply re-proves, under the exclusive lock, the durable state a
// fresh apply was planned from and the input its plan was compiled from, since
// an operation registered over another input would carry a plan its recorded
// revision and digest do not describe. It returns the completed operation the
// apply follows, if any.
func (s Service) verifyFreshApply(ctx context.Context, tx Transaction, store OperationStore, decided transition) (operationstore.Operation, error) {
	changed := func(current basis) error { return contextChanged(decided.verb, decided.basis, current) }
	previous, _, _, err := s.verifyBasis(ctx, store, decided.basis, changed)
	if err != nil {
		return operationstore.Operation{}, err
	}
	current := decided.basis
	current.revision, current.input = tx.Identity().Revision, inputDigest(tx)
	if current.revision != decided.basis.revision || current.input != decided.basis.input {
		return operationstore.Operation{}, changed(current)
	}
	return previous, nil
}

// publish registers the operation and records whether it did. A registration
// that failed is read back: an index that provably does not name the operation
// registered nothing, and one that names it, or that cannot be read, may have.
func (s Service) publish(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, record *registering) error {
	err := store.Register(ctx, operation, plan)
	if err == nil {
		record.outcome = registered
		return nil
	}
	if index, read := store.Index(recordingContext(ctx)); read != nil || index.Current == operation.ID {
		record.outcome = possiblyRegistered
	}
	return err
}

// unregistered gives back what a registration that provably did not happen
// raised, and reports cause with any restoration that failed beside it. The
// binding goes only when this invocation created it: a removal reopens its
// apply's binding and a continuation its operation's, and releasing either
// would leave effects no later removal can present the material for. A
// release that fails leaves the binding for the next registration or destroy
// to collect, so it never hides the failure being reported.
func (s Service) unregistered(ctx context.Context, name string, decided transition, binding string, record *registering, cause error) error {
	recording := recordingContext(ctx)
	if decided.fresh && decided.reopen == "" && binding != "" {
		_, _ = s.binder.Release(recording, custody.BindingRequest{ContextName: name, BindingID: binding})
	}
	return beside(cause, s.restoreEvidence(recording, name, record))
}

// restoreEvidence publishes, in its own transaction, the evidence the index
// implies now, once the running evidence is this invocation's to give back and
// while no newer claim has taken it since: the projection of the operation the
// index names, read again, or pristine for none. Running evidence stays while
// the index names no operation or a completed removal and the context holds a
// reservation, because those reservations are what an interrupted registration
// published and no operation owns them: the evidence keeps the context from
// being deleted until a destroy or the next apply releases them.
func (s Service) restoreEvidence(ctx context.Context, name string, record *registering) error {
	if !record.raised {
		return nil
	}
	return s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		claimed, err := store.Claimed(ctx)
		if err != nil || record.newer(claimed) {
			return err
		}
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		verb, state := reconciliation.Destroy, reconciliation.OperationDone
		if index.Current != "" {
			operation, err := store.ReadOperation(ctx, index.Current)
			if err != nil {
				return err
			}
			verb, state = operation.Verb, operation.State
		}
		if verb == reconciliation.Destroy && state == reconciliation.OperationDone && holdsReservation(tx) {
			return nil
		}
		return s.settle(ctx, tx, verb, state)
	})
}

// beside reports a failed restoration after the error that caused it.
func beside(cause, restoration error) error {
	if restoration == nil {
		return cause
	}
	if len(diagnostics.Of(cause)) != 0 && len(diagnostics.Of(restoration)) != 0 {
		return withCause(cause, restoration)
	}
	return errors.Join(cause, restoration)
}

// held reads the bindings a context holds before an invocation's first
// transaction. Collecting them is housekeeping no transition depends on, so a
// listing that fails collects nothing rather than refusing a transition that
// may need no material from that store at all.
func (s Service) held(ctx context.Context, name string) []string {
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil {
		return nil
	}
	return listed
}

// collect releases every binding the context held before this invocation
// began that it neither keeps nor released itself. Only an invocation whose
// registration or pristine publication moved the index or lowered the
// evidence collects, so each in-flight transition that might still register
// one of these bindings refuses at its own re-proof. The releases are made
// even once the invocation is interrupted, and a release that fails leaves its
// binding for the next registration or destroy.
func (s Service) collect(ctx context.Context, name string, held, keep []string) {
	for _, binding := range held {
		if !slices.Contains(keep, binding) {
			_, _ = s.binder.Release(recordingContext(ctx), custody.BindingRequest{ContextName: name, BindingID: binding})
		}
	}
}

// unclaimed decides a destroy of a context holding no operation. It settles
// while nothing claims the context: pristine evidence and no reservation.
// Running evidence, or a reservation beside pristine evidence, is what a
// registration interrupted before the index named its operation leaves, so the
// destroy releases it. Any other evidence, or an unindexed operation directory
// that lists block records, is state no index accounts for, which the destroy
// refuses without writing anything.
func unclaimed(ctx context.Context, view View, store OperationStore) (transition, error) {
	pristine, err := projection(reconciliation.Destroy, reconciliation.OperationDone)
	if err != nil {
		return transition{}, err
	}
	running, err := projection(reconciliation.Apply, reconciliation.OperationRunning)
	if err != nil {
		return transition{}, err
	}
	evidence := view.Evidence()
	switch {
	case bytes.Equal(evidence, pristine) && !holdsReservation(view):
		return transition{noop: true, verb: reconciliation.Destroy}, nil
	case !bytes.Equal(evidence, pristine) && !bytes.Equal(evidence, running):
		return transition{}, unaccounted()
	}
	if err := refuseStarted(ctx, store); err != nil {
		return transition{}, err
	}
	return transition{unclaimed: true, verb: reconciliation.Destroy, basis: basis{evidence: evidence}}, nil
}

// refuseStarted refuses when any operation directory lists a block record: no
// index names it, so it is an operation whose index was lost rather than a
// claim or an interrupted registration, and its effects may be on a host.
func refuseStarted(ctx context.Context, store OperationStore) error {
	claimed, err := store.Claimed(ctx)
	if err != nil {
		return err
	}
	for _, directory := range claimed {
		started, err := store.Started(ctx, directory)
		if err != nil {
			return err
		}
		if started {
			return unaccounted()
		}
	}
	return nil
}

func unaccounted() error {
	return failure("lifecycle.state",
		"the context holds operation records or evidence that no index names",
		"review its durable state with bootwright status")
}

// releaseUnclaimed gives back what an interrupted registration left. Under the
// exclusive lock it re-proves that the context still holds no operation, the
// evidence it was decided from and no started operation directory, then
// releases the context's reservations and publishes pristine evidence; only
// then does it release every binding the context held before it began. Any
// fresh apply still in flight raised the evidence this lowers, or claims a
// directory that raises it again, so it refuses before it can register a
// binding released here.
func (s Service) releaseUnclaimed(ctx context.Context, name string, decided transition) error {
	held := s.held(ctx, name)
	err := s.workspace.MutateLifecycle(ctx, name, func(tx Transaction) error {
		store := s.store(tx)
		changed := func(current basis) error { return contextChanged(decided.verb, decided.basis, current) }
		if _, _, _, err := s.verifyBasis(ctx, store, decided.basis, changed); err != nil {
			return err
		}
		if current := tx.Evidence(); !bytes.Equal(current, decided.basis.evidence) {
			moved := decided.basis
			moved.evidence = current
			return changed(moved)
		}
		if err := refuseStarted(ctx, store); err != nil {
			return err
		}
		if err := releaseHeld(ctx, tx); err != nil {
			return err
		}
		return s.settle(ctx, tx, reconciliation.Destroy, reconciliation.OperationDone)
	})
	if err != nil {
		return err
	}
	s.collect(ctx, name, held, nil)
	return nil
}
