package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// frozenBindings are the Secret bindings a transition reopens, none of which anything stands in for.
func frozenBindings(decided operationPlan) []string {
	switch {
	case decided.noop, decided.finalize, decided.unclaimed:
		return nil
	case decided.verb == reconciliation.Destroy:
		return decided.presented()
	case decided.fresh && decided.restores != "":
		return decided.presents.bindings()
	case decided.restoring():
		held := joinBindings(decided.presents.bindings(), ownBinding(decided.operation))
		slices.Sort(held)
		return held
	case decided.fresh:
		return joinBindings(nil, decided.reopen)
	}
	return joinBindings(nil, firstBinding(decided.operation.Bindings))
}

func (t operationPlan) presenter(block string) string {
	if _, own := t.plan.Block(block); t.verb == reconciliation.Destroy || t.restoring() && (t.fresh || !own) {
		return t.presents.of(block)
	}
	return ownBinding(t.operation)
}

// refuseLostBinding refuses, before anything else, a transition whose frozen
// binding the keyring no longer lists; a failed listing proves nothing.
func (s Service) refuseLostBinding(ctx context.Context, name string, decided operationPlan, read basis) error {
	frozen := frozenBindings(decided)
	if len(frozen) == 0 {
		return nil
	}
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil {
		return nil
	}
	for _, binding := range frozen {
		if !slices.Contains(listed, binding) {
			return s.lostBinding(ctx, name, decided, read, binding, "which the context's keyring no longer lists")
		}
	}
	return nil
}

// unreopenable reports a reopen of the frozen binding that failed. The binding
// is lost when the keyring cannot read its material or no longer lists it;
// anything else, a keyring that cannot be listed included, is reported as it
// failed.
func (s Service) unreopenable(ctx context.Context, name string, decided operationPlan, binding string, cause error) error {
	if ctx.Err() != nil {
		return cause
	}
	if why, unreadable := unreadableMaterial(cause); unreadable {
		return s.lostBinding(ctx, name, decided, decided.basis, binding, why)
	}
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil || slices.Contains(listed, binding) {
		return cause
	}
	return s.lostBinding(ctx, name, decided, decided.basis, binding, "which the context's keyring no longer lists")
}

// unreadableMaterial says why a keyring failure proves a binding lost: the
// keyring reports its material corrupt or undecryptable. Anything else proves
// nothing about the binding.
func unreadableMaterial(err error) (string, bool) {
	if reported := diagnostics.Of(err); len(reported) != 0 {
		switch reported[0].Code {
		case "secret.store.corrupt", "secret.store.crypto":
			return "whose material the context's keyring cannot read: " + reported[0].Message, true
		}
	}
	return "", false
}

// lostBinding refuses the transition once the context still holds the
// operation it was decided over, in the state it read. Only an invocation that
// moved the context on releases a binding an operation names, so a binding
// released since the decision reports that change instead.
func (s Service) lostBinding(ctx context.Context, name string, decided operationPlan, read basis, binding, why string) error {
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
			return s.contextChanged(decided, read, current)
		}
		return nil
	})
	if err != nil {
		return err
	}
	objects := objectsOf(presentedBy(decided, binding))
	repeat := contextCommand(name, string(decided.verb), invocationFlags(decided.floor(), reconciliation.Selection{}, nil)...)
	if decided.verb == reconciliation.Apply {
		repeat = s.preparationRetry(name, repeat)
	}
	return engineBindingLost.Fail(
		lostBindingMessage(decided.basis.record, binding, why, objects),
		"restore the context's keyring from a complete backup and repeat "+repeat+
			", or run "+lostBindingExit(name)+" and then remove the objects named here by hand")
}

func presentedBy(decided operationPlan, binding string) reconciliation.Plan {
	held := decided.plan
	if !decided.fresh {
		held = coveredBy(decided.operation, decided.plan, decided.states)
	}
	switch {
	case decided.restoring():
		kept := reconciliation.Plan{Verb: reconciliation.Apply}
		if !decided.fresh && binding == ownBinding(decided.operation) {
			kept.Blocks = slices.Clone(held.Blocks)
		}
		for _, block := range decided.keptSteps() {
			if decided.presents.of(block.ID) == binding {
				kept.Blocks = append(kept.Blocks, block)
			}
		}
		return kept
	case decided.verb == reconciliation.Destroy:
		for _, block := range decided.keptSteps() {
			if block.Kind == string(api.Machine) {
				held.Blocks = append(slices.Clone(held.Blocks), block)
			}
		}
		held.Blocks = slices.DeleteFunc(slices.Clone(held.Blocks), func(block reconciliation.Block) bool { return decided.presents.of(block.ID) != binding })
	}
	return held
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

// frozenReopen is each binding status checks with the objects it presents; a
// destroy whose blocks are all done names none, since it only finalizes.
type frozenReopen struct {
	operation operationstore.Operation
	reopens   []reopened
	finalizes bool
}

type reopened struct {
	binding string
	objects []string
}

func (f frozenReopen) bindings() []string {
	var held []string
	for _, reopen := range f.reopens {
		held = append(held, reopen.binding)
	}
	return held
}

func reopensOf(presents presenting, plan reconciliation.Plan) []reopened {
	var reopens []reopened
	for _, binding := range presents.bindings() {
		held := reconciliation.Plan{Blocks: slices.DeleteFunc(slices.Clone(plan.Blocks), func(block reconciliation.Block) bool { return presents.of(block.ID) != binding })}
		if len(held.Blocks) != 0 || binding == presents.lineage {
			reopens = append(reopens, reopened{binding: binding, objects: objectsOf(held)})
		}
	}
	return reopens
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
	if trimmed(operation) {
		return trimmedReopen(ctx, store, operation, plan, states)
	}
	held := standingOf(operation, plan, states)
	own := classify(operation.Verb, held)
	if operation.Verb == reconciliation.Destroy && (own == transitionSettle || own == transitionRefuse || !held.pending) {
		return frozenReopen{}, nil
	}
	presents := presenting{lineage: firstBinding(operation.Bindings)}
	switch {
	case operation.Verb == reconciliation.Destroy && operation.Source != "":
		folded, err := removalLineage(ctx, store, lineageMember{operation: operation, plan: plan, states: states})
		if err != nil {
			return frozenReopen{}, err
		}
		presents = folded.presenting()
	case operation.Verb == reconciliation.Apply && operation.Predecessor != "":
		if presents, err = appliedPresenting(ctx, store, operation); err != nil {
			return frozenReopen{}, err
		}
	}
	if presents.lineage == "" && len(presents.steps) == 0 {
		return frozenReopen{}, nil
	}
	return frozenReopen{
		operation: operation, reopens: reopensOf(presents, coveredBy(operation, plan, states)),
		finalizes: operation.Verb == reconciliation.Apply && own == transitionFinalize,
	}, nil
}

// nameLostBinding replaces the next steps status offers once the binding the
// current operation's continuation or removal reopens is lost, and names why
// among the contradictions, in the refusal's words. The binding is lost when
// the keyring no longer lists it, or when its listing fails as the reopen
// would, reporting the material corrupt or undecryptable: every keyring
// session reads the same metadata and artifacts, so the reopen fails the same
// way. Any other listing failure names nothing. The steps are the apply that
// finalizes an apply whose blocks are all done, which reopens nothing, and
// then the deletion that abandons what the operation owns.
func (s Service) nameLostBinding(ctx context.Context, name string, result *StatusResult, frozen frozenReopen) {
	if len(frozen.reopens) == 0 {
		return
	}
	why := "which the context's keyring no longer lists"
	listed, err := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
	if err != nil {
		reason, unreadable := unreadableMaterial(err)
		if !unreadable {
			return
		}
		why = reason
	}
	for _, reopen := range frozen.reopens {
		if err == nil && slices.Contains(listed, reopen.binding) || reopen.binding == "" {
			continue
		}
		result.Contradictions = append(result.Contradictions, lostBindingMessage(frozen.operation, reopen.binding, why, reopen.objects))
		result.NextSteps = []string{lostBindingExit(name)}
		if frozen.finalizes {
			result.NextSteps = []string{contextCommand(name, string(reconciliation.Apply)), lostBindingExit(name)}
		}
		return
	}
}
