package lifecycle

import (
	"bytes"
	"context"
	"maps"
	"reflect"
	"slices"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// proveShared re-proves, under the shared lock and before the plan is
// presented, authorized or confirmed, what the transition's exclusive
// transaction proves again before it registers: a continuation or resolution
// its input, other-build blocks, closure, setup, host binding and runtime,
// rebuilt if missing; a fresh removal its host. No plan is then presented for
// a transition that would refuse. A lost frozen binding names the only exit
// those records leave whatever else they hold, so it wins: a binding the
// keyring no longer lists has refused already, and one whose material the
// keyring cannot read, which only its reopen proves, is reported in place of
// the refusal here, as the reopen this transition would make reports it.
func (s Service) proveShared(ctx context.Context, name string, decided operationPlan) error {
	removal := decided.fresh && decided.verb == reconciliation.Destroy
	if !removal && (decided.fresh || decided.operation.ID == "") {
		return nil
	}
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		if removal {
			return s.verifyRemovalHost(ctx, view)
		}
		if err := s.verifyContinuation(ctx, view, decided.operation, decided.sameBlocks, nil); err != nil {
			return err
		}
		_, err := s.approvedRuntime(ctx)
		return err
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
	if err := s.prerequisitesRefusal(ctx, view, operation); err != nil {
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
// and interpreter ABI, differs from this build's; nil located is read from the
// setup receipt alone, so a decision never rebuilds a missing runtime.
func (s Service) closureRefusal(ctx context.Context, view View, operation operationstore.Operation, located *approved) error {
	if located == nil {
		current, err := s.receiptRuntime(ctx)
		if err != nil {
			return err
		}
		located = &current
	}
	current := located.closure
	if current != *operation.Closure {
		frozen := *operation.Closure
		remediation := "run the controller install of " + registeredWith(operation.Executable) + ", which restores its runtime (Python " + frozen.Python + ", ansible-core " + frozen.Ansible +
			"), fetching any wheel its cache no longer holds over the network or with --wheel-mirror <mirror>, then continue it with that build"
		if supersedable(operation) {
			remediation += ", or take back what it owns with " + contextCommand(view.Identity().Name, string(reconciliation.Destroy))
		}
		return failure("lifecycle.state", "this build's controller runtime carries another Python and Ansible closure (Python "+
			current.Python+", ansible-core "+current.Ansible+") than the one this operation registered with", remediation)
	}
	return nil
}

// verifyHostBinding proves this context is bound to the host the operation
// runs on and that its setup completed, before any local effect. unbound is
// the remedy when the controller state records no binding for the context,
// and admitUnbound admits that context instead, once the host is proved.
func verifyHostBinding(view prerequisites.StorageView, identity ContextIdentity, host controller.InstalledHostIdentity, unbound string, admitUnbound bool) error {
	if !view.Exists || !view.Prepared {
		return failure("controller.identity", "this host has no completed controller setup", "run bootwright controller install")
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
		return failure("controller.identity", "this host is not the host the context is bound to", controller.HostMismatchRemedy(identity.Name))
	}
	for _, binding := range view.State.Bindings {
		if binding.Context != identity.Name {
			continue
		}
		if binding.HostDigest != digest {
			return failure("controller.identity", "the recorded controller binding does not match this host", controller.HostMismatchRemedy(identity.Name))
		}
		return nil
	}
	if admitUnbound {
		return nil
	}
	return failure("controller.identity", "this context is not bound to a controller host", unbound)
}
