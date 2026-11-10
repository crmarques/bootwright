package lifecycle

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

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
func (s Service) register(ctx context.Context, tx Transaction, store OperationStore, decided operationPlan, binding string, material map[string]secrets.Material, record *registering, located approved) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
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
	if decided.predecessor == "" {
		if err := s.reserve(ctx, tx, decided, material); err != nil {
			return fail(err)
		}
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
		Source:     decided.source, Predecessor: decided.predecessor, Bindings: bindings, State: reconciliation.OperationRunning,
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
func (s Service) resume(ctx context.Context, tx Transaction, store OperationStore, decided operationPlan, record *registering, located approved) (operationstore.Operation, reconciliation.Plan, *operationstore.Log, error) {
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
	rebound := decided.rebound.binding != "" && !slices.Equal(operation.Bindings, []string{decided.rebound.binding})
	if err == nil && (rebound || operation.State != reconciliation.OperationRunning) {
		operation.State = reconciliation.OperationRunning
		if rebound {
			operation.Bindings = []string{decided.rebound.binding}
		}
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
func (s Service) reprove(ctx context.Context, tx Transaction, store OperationStore, decided operationPlan) error {
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
	if decided.predecessor != "" {
		return s.proveHostBinding(ctx, tx, previous)
	}
	return s.establishBinding(ctx, tx, decided.binding.controller)
}

func (s Service) proveHostBinding(ctx context.Context, tx Transaction, previous operationstore.Operation) error {
	host, err := s.host.Identity(ctx)
	if err != nil {
		return err
	}
	return verifyHostBinding(tx.Controller(), tx.Identity(), host, unboundExit(tx, previous), false)
}

// reserve claims the exclusive host resources the plan needs before any effect.
// A destroy needs no new claim and releases at completion, so it refuses when
// that release would strand another context's use of what it removes.
func (s Service) reserve(ctx context.Context, tx Transaction, decided operationPlan, material map[string]secrets.Material) error {
	if decided.verb == reconciliation.Destroy {
		return reproveRelease(ctx, tx, "", "")
	}
	var controllerKeys []string
	if len(decided.binding.hostKeyed) != 0 {
		keys, err := controllerKeyQualifiers(ctx, s.options.HostKeys)
		if err != nil {
			return err
		}
		controllerKeys = keys
	}
	planned, err := hostKeyedReservations(decided.binding, material, controllerKeys)
	if err != nil {
		return err
	}
	return tx.Reserve(ctx, keepProved(tx.Controller().State.Reservations, tx.Identity().Name, planned))
}

func reproveRelease(ctx context.Context, tx Transaction, kind, object string) error {
	var releasing func(prerequisites.HostReservation) bool
	if kind != "" {
		releasing = func(reservation prerequisites.HostReservation) bool {
			return prerequisites.Releases(reservation, kind, object)
		}
	}
	return tx.ReproveReservations(ctx, func(held []prerequisites.HostReservation) error {
		return prerequisites.ReleaseFailure(held, tx.Identity().Name, kind, object)
	}, releasing)
}

func keepProved(held []prerequisites.HostReservation, contextName string, planned []prerequisites.HostReservation) []prerequisites.HostReservation {
	kept := make([]prerequisites.HostReservation, 0, len(planned))
	for _, reservation := range planned {
		reservation.Keys = slices.Clone(reservation.Keys)
		for _, own := range held {
			if own.Context != contextName || own.Kind != reservation.Kind || own.Service != reservation.Service || own.Shared != reservation.Shared {
				continue
			}
			for _, key := range own.Keys {
				if (strings.HasPrefix(key, "bmc-system:") || strings.HasPrefix(key, "bmc-mac:")) && !slices.Contains(reservation.Keys, key) {
					reservation.Keys = append(reservation.Keys, key)
				}
			}
		}
		kept = append(kept, reservation)
	}
	return kept
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
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright controller install")
	}
	return tx.Bind(ctx, machine, host)
}
