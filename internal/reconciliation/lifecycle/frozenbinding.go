package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// frozenBinding is the Secret binding a transition reopens: the one its
// operation froze for a continuation, and the one the operation it supersedes
// or removes froze for a fresh removal. Nothing stands in for it, so a lost one
// leaves that operation with neither. A fresh apply binds its own, and a
// finalization, a settled verb and a destroy of a context holding no operation
// reopen none.
func frozenBinding(decided transition) string {
	switch {
	case decided.noop, decided.finalize, decided.unclaimed:
		return ""
	case decided.fresh:
		return decided.reopen
	}
	return firstBinding(decided.operation.Bindings)
}

// refuseLostBinding refuses a transition whose frozen binding the keyring no
// longer lists, before it is authorized, presented or confirmed and before it
// registers, reserves or releases anything. A listing that fails proves
// nothing, so the reopen decides then. read is the durable state the decision
// was read from.
func (s Service) refuseLostBinding(ctx context.Context, name string, decided transition, read basis) error {
	binding := frozenBinding(decided)
	if binding == "" {
		return nil
	}
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil || slices.Contains(listed, binding) {
		return nil
	}
	return s.lostBinding(ctx, name, decided, read, binding, "which the context's keyring no longer lists")
}

// unreopenable reports a reopen of the frozen binding that failed. The binding
// is lost when the keyring cannot read its material or no longer lists it;
// anything else, a keyring that cannot be listed included, is reported as it
// failed.
func (s Service) unreopenable(ctx context.Context, name string, decided transition, binding string, cause error) error {
	if ctx.Err() != nil {
		return cause
	}
	if reported := diagnostics.Of(cause); len(reported) != 0 {
		switch reported[0].Code {
		case "secret.store.corrupt", "secret.store.crypto":
			return s.lostBinding(ctx, name, decided, decided.basis, binding, "whose material the context's keyring cannot read: "+reported[0].Message)
		}
	}
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil || slices.Contains(listed, binding) {
		return cause
	}
	return s.lostBinding(ctx, name, decided, decided.basis, binding, "which the context's keyring no longer lists")
}

// lostBinding refuses the transition once the context still holds the
// operation it was decided over, in the state it read. Only an invocation that
// moved the context on releases a binding an operation names, so a binding
// released since the decision reports that change instead.
func (s Service) lostBinding(ctx context.Context, name string, decided transition, read basis, binding, why string) error {
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		current := basis{operation: index.Current}
		if current.operation != "" {
			operation, err := store.ReadOperation(ctx, current.operation)
			if err != nil {
				return err
			}
			current.state, current.record = operation.State, operation
		}
		if current.operation != read.operation || current.state != read.state {
			return contextChanged(decided.verb, read, current)
		}
		return nil
	})
	if err != nil {
		return err
	}
	objects := objectsOf(decided.plan)
	if !decided.fresh {
		objects = objectsOf(ownedBy(decided.operation.Verb, decided.plan, decided.states))
	}
	return failure("lifecycle.state",
		lostBindingMessage(decided.basis.record, binding, why, objects),
		"restore the context's keyring from a complete backup and repeat bootwright "+string(decided.verb)+
			", or run "+lostBindingExit(name)+" and then remove the objects named here by hand")
}

// lostBindingMessage names the operation that froze the binding, the binding,
// why it cannot be reopened and every object that operation owns, because
// abandoning them is the other exit and nothing lists them once the context is
// deleted.
func lostBindingMessage(operation operationstore.Operation, binding, why string, objects []string) string {
	owned := "that operation, which owns no object"
	if len(objects) != 0 {
		owned = "what that operation owns: " + strings.Join(objects, ", ")
	}
	return "the " + string(operation.Verb) + " " + operation.ID + " (" + string(operation.State) + ") froze the Secret binding " + binding +
		", " + why + "; no other material stands in for it, so nothing continues or removes " + owned
}

// lostBindingExit is the deletion that abandons what an operation owns once its
// frozen binding is lost.
func lostBindingExit(name string) string {
	return "bootwright context delete --name " + name + " --purge --allow-orphans"
}

// ownedBy is what an operation still owns: every block an apply started, or
// every block a removal has not yet proved gone.
func ownedBy(verb reconciliation.Verb, plan reconciliation.Plan, states map[string]reconciliation.BlockState) reconciliation.Plan {
	if verb == reconciliation.Apply {
		return reconciliation.OwnedSubset(plan, states)
	}
	return reconciliation.RemainingSubset(plan, states)
}

// objectsOf names each API object a plan's blocks realize, once, in bytewise
// order, so a removal and the operation it removes name the same objects.
func objectsOf(plan reconciliation.Plan) []string {
	objects := make([]string, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		objects = append(objects, block.Kind+"/"+block.Object)
	}
	slices.Sort(objects)
	return slices.Compact(objects)
}

// frozenReopen is what status needs to tell whether the current operation's
// frozen binding is lost: the operation, the binding its continuation or a
// fresh removal of it reopens, and the objects it owns. A completed destroy
// names none, and neither does one whose blocks are all done: only its own
// verb acts on it, and that verb finalizes it, which reopens nothing.
type frozenReopen struct {
	operation operationstore.Operation
	binding   string
	objects   []string
}

func frozenReopenOf(ctx context.Context, store OperationStore) (frozenReopen, error) {
	index, err := store.Index(ctx)
	if err != nil || index.Current == "" {
		return frozenReopen{}, err
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return frozenReopen{}, err
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return frozenReopen{}, err
	}
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		return frozenReopen{}, err
	}
	if operation.Verb == reconciliation.Destroy && (operation.State == reconciliation.OperationDone || !pendingRemains(plan, states)) {
		return frozenReopen{}, nil
	}
	binding := firstBinding(operation.Bindings)
	if operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationFailed && operation.Source != "" {
		applied, err := store.ReadOperation(ctx, operation.Source)
		if err != nil {
			return frozenReopen{}, err
		}
		if inherited := firstBinding(applied.Bindings); inherited != "" {
			binding = inherited
		}
	}
	if binding == "" {
		return frozenReopen{}, nil
	}
	return frozenReopen{operation: operation, binding: binding, objects: objectsOf(ownedBy(operation.Verb, plan, states))}, nil
}

// nameLostBinding makes the deletion that abandons what the current operation
// owns the only next step status offers once the keyring no longer lists the
// binding that operation's continuation or removal reopens, and names why among
// the contradictions, in the refusal's words. A listing that fails names
// nothing.
func (s Service) nameLostBinding(ctx context.Context, name string, result *StatusResult, frozen frozenReopen) {
	if frozen.binding == "" {
		return
	}
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil || slices.Contains(listed, frozen.binding) {
		return
	}
	result.Contradictions = append(result.Contradictions,
		lostBindingMessage(frozen.operation, frozen.binding, "which the context's keyring no longer lists", frozen.objects))
	result.NextSteps = []string{lostBindingExit(name)}
}
