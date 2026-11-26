package lifecycle

import (
	"context"
	"maps"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// execute binds the Secrets the plan consumes, then performs the operation
// inside one held transaction. Binding happens first because acquiring
// confidential material takes the same store lock the transaction holds. A
// fresh apply raises its running evidence and claims its operation directory
// before it binds, so nothing it binds, claims or reserves is ever held under
// evidence that lets the context be updated or deleted.
func (s Service) execute(ctx context.Context, name string, decided operationPlan) (*OperationResult, error) {
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
		s.startCheck = nil
	}
	rebound, err := s.rebind(ctx, name, decided)
	if err != nil {
		return nil, s.unregistered(ctx, name, decided, "", record, err)
	}
	decided = rebound
	if len(decided.rebound) != 0 {
		s.startCheck = nil
	}
	opened, err := s.bind(ctx, name, decided)
	if err != nil {
		return nil, s.unregistered(ctx, name, decided, opened.created, record, err)
	}
	defer opened.material.clear()
	if err := s.proveBindings(ctx, name, decided, opened.material); err != nil {
		return nil, s.unregistered(ctx, name, decided, opened.created, record, err)
	}
	var result *OperationResult
	var completion removalCompletion
	var unreleased error
	var kept []string
	var keptErr error
	err = s.mutateAdmitted(ctx, name, s.startCheck, func(tx Transaction) error {
		result, err = s.run(ctx, tx, decided, opened, record)
		if decided.fresh && decided.verb == reconciliation.Apply && record.outcome == registered {
			kept, keptErr = lineageBindings(recordingContext(ctx), s.store(tx), record.operation)
		}
		// The state a completed removal's record left is captured here, after
		// finish wrote it, because the pristine publication re-proves exactly
		// that once the releases are done. What it gives back is what its
		// transition releases and the binding this invocation reopened. A
		// removal whose record reads done owes both even once the invocation
		// is interrupted, so they run under the recording boundary.
		if completedRemoval(decided, result) && !decided.staged() {
			completion, unreleased = captureRemoval(recordingContext(ctx), s.store(tx), result.Receipt.Operation, joinBindings(slices.Clone(decided.release), opened.recorded...))
		}
		return err
	})
	switch record.outcome {
	case notRegistered:
		return nil, s.unregistered(ctx, name, decided, opened.created, record, err)
	case possiblyRegistered:
		// The index may name the operation, so it may own this binding and its
		// running evidence for its whole lifetime: every later attempt reopens
		// the one and needs the other. Both stay, and nothing is collected.
		return result, err
	}
	collect := decided.fresh && !decided.staged()
	// A completed removal no longer needs the material its apply bound, even
	// when a log fault it latched makes the invocation report a failure. Its
	// receipt stays done whether or not everything it owned was given back.
	if completedRemoval(decided, result) && !decided.staged() {
		if unreleased == nil {
			unreleased = s.completeRemoval(recordingContext(ctx), name, completion)
		}
		if unreleased != nil {
			err, collect = withCause(err, incompleteRemoval(name, unreleased)), false
		}
	}
	if collect && keptErr == nil {
		s.collect(ctx, name, held, joinBindings(joinBindings(slices.Clone(decided.release), opened.recorded...), kept...))
	}
	return result, err
}

type bound struct {
	created  string
	recorded []string
	material stepMaterial
}

// bind binds a fresh apply's Secrets and reopens each step's presenting binding.
func (s Service) bind(ctx context.Context, name string, decided operationPlan) (bound, error) {
	held := bound{material: stepMaterial{bound: map[string]map[string]secrets.Material{}}}
	if decided.fresh && decided.verb == reconciliation.Apply && decided.reopen == "" && len(decided.binding.secrets) != 0 {
		result, err := s.binder.Bind(ctx, custody.BindRequest{ContextName: name, Names: decided.binding.secrets})
		if err != nil {
			return held, err
		}
		held.created = result.ID
	}
	var reopen []string
	switch {
	case decided.verb == reconciliation.Destroy:
		held.material.base, held.material.steps = decided.presents.lineage, decided.presents.steps
		reopen = decided.presented()
		held.recorded = decided.recorded(reopen...)
	case decided.restoring():
		held.material.base = held.created
		if !decided.fresh {
			held.material.base = ownBinding(decided.operation)
		}
		held.material.steps = map[string]string{}
		for _, block := range decided.keptSteps() {
			held.material.steps[block.ID] = decided.presents.of(block.ID)
		}
		reopen = joinBindings(decided.presents.bindings(), held.material.base)
		held.recorded = decided.recorded(held.created)
	case decided.fresh:
		held.material.base = held.created
		held.recorded = joinBindings([]string{}, held.created)
		reopen = joinBindings(nil, held.created)
	default:
		held.material.base = firstBinding(decided.operation.Bindings)
		held.recorded, reopen = joinBindings([]string{}, held.material.base), joinBindings(nil, held.material.base)
	}
	if failed, err := s.reopenEach(ctx, name, reopen, held.material); err != nil {
		held.material.clear()
		held.material = stepMaterial{}
		if slices.Contains(frozenBindings(decided), failed) {
			err = s.unreopenable(ctx, name, decided, failed, err)
		}
		return held, err
	}
	return held, nil
}

func clearMaterial(material map[string]secrets.Material) {
	for _, value := range material {
		value.Clear()
	}
}

// run joins every adapter before releasing its execution scope and transaction.
func (s Service) run(ctx context.Context, tx Transaction, decided operationPlan, opened bound, record *registering) (*OperationResult, error) {
	material := opened.material
	if err := s.admitApplyView(ctx, tx, decided); err != nil {
		return nil, err
	}
	if s.options.BeginInvocation != nil {
		var release func()
		ctx, release = s.options.BeginInvocation(ctx)
		defer release()
	}
	store := s.store(tx)
	// Admission proves quiescence and determines any pending source proofs.
	// Proofs, registration, and attempts share the located runtime whose closure
	// the operation freezes.
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
	stopGroups, deferred, err := s.removalStart(ctx, tx, store, located, decided, material)
	if err != nil {
		return nil, err
	}
	decided.deferredSourceProofs = slices.Sorted(maps.Keys(deferred))
	operation, plan, log, err := s.register(ctx, tx, store, decided, opened, record, located)
	if err != nil {
		return nil, err
	}
	result := &OperationResult{Context: tx.Identity(), Verb: string(operation.Verb), Steps: steps(plan, nil), KeepGoing: s.keepGoing, Scope: decided.scope()}
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
	for _, rebound := range decided.rebound {
		if err := logging.append(ctx, log, rebound.record()); err != nil {
			return result, err
		}
	}
	// The location is named before the first effect, because its whole purpose
	// is to be followed while the work runs.
	result.LogLocation = store.LogDirectory(operation.ID)
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		return result, err
	}
	run := invocation{
		stopGroups: stopGroups, deferredSource: deferred, tx: tx, store: store, approved: located, boundary: logging,
		operation: operation, plan: plan, material: material, unregistered: frozenUnregistered(decided.plan), floor: decided.floor(),
	}
	var refusal error
	if operation.Verb == reconciliation.Apply {
		run.effects, _ = reachableEffects(decided.plan, decided.states, decided.selection)
		refusal = s.checkApplyStart(work, run, states)
	}
	boundary := true
	var cause error
	if refusal == nil {
		if s.options.Progress != nil && result.LogLocation != "" {
			s.options.Progress.ReportLogLocation(ctx, LogLocation{Path: result.LogLocation, Context: result.Context.Name, Operation: operation.ID})
		}
		boundary, result.Warnings, cause = s.converge(work, run, decided.phases, states, decided.selection)
	} else {
		for _, state := range states {
			if state == reconciliation.BlockRunning {
				boundary = false
				break
			}
		}
	}
	logging.close(ctx, log)
	final, terminal := s.finish(recordingContext(ctx), tx, store, operation, plan, states, decided.selection, boundary, logging.faulted(), result)
	if result.KeepGoing && len(result.Blocks) != 0 && result.Receipt.State != string(reconciliation.OperationDone) {
		result.RetiringWithHosts = pendingRetirements(plan, decided.phases, states)
	}
	err = withCause(logging.err(), withCause(cause, terminal))
	for _, block := range plan.Blocks {
		err = withCause(deferred[block.ID], err)
	}
	if refusal != nil {
		err = beside(refusal, err)
	}
	return final, err
}
