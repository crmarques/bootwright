package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// removalScope is what a fresh removal takes back and the Secret bindings it
// reopens and releases; started counts the blocks the lineage's head started.
type removalScope struct {
	owned      reconciliation.Plan
	started    int
	reopen     string
	release    []string
	operations []string
}

func removalOver(ctx context.Context, store recordReader, applied operationstore.Operation, frozen, started reconciliation.Plan) (removalScope, error) {
	scope := removalScope{owned: started, started: len(started.Blocks), reopen: firstBinding(applied.Bindings), release: applied.Bindings}
	if applied.Predecessor == "" {
		return scope, nil
	}
	held, err := lineageOf(ctx, store, lineageMember{operation: applied, plan: frozen})
	if err != nil {
		return removalScope{}, err
	}
	plans := make([]reconciliation.Plan, 0, len(held))
	for _, member := range held[:len(held)-1] {
		plans = append(plans, member.plan)
	}
	if scope.owned, err = reconciliation.Lineage(append(plans, started)...); err != nil {
		return removalScope{}, unremovable(applied, err)
	}
	scope.reopen, scope.release = firstBinding(held[0].operation.Bindings), held.bindings()
	scope.operations = held.operationIDs()
	return scope, nil
}

func unremovable(applied operationstore.Operation, cause error) error {
	message := "the lineage of the apply " + applied.ID + " cannot be taken back as one removal"
	if reported := diagnostics.Of(cause); len(reported) != 0 {
		message += ": " + reported[0].Message
	}
	return failure("lifecycle.state", message, "restore the context store")
}

func replacementOf(ctx context.Context, store recordReader, removal operationstore.Operation, remaining reconciliation.Plan) (removalScope, error) {
	scope := removalScope{owned: remaining, started: len(remaining.Blocks), reopen: firstBinding(removal.Bindings), release: slices.Clone(removal.Bindings)}
	if removal.Source == "" {
		return scope, nil
	}
	held, err := sourceLineage(ctx, store, removal.Source)
	if err != nil {
		return removalScope{}, err
	}
	scope.release = joinBindings(scope.release, held.released()...)
	scope.operations = held.operationIDs()
	if scope.reopen == "" {
		scope.reopen = firstBinding(held[0].operation.Bindings)
	}
	return scope, nil
}

func (l lineage) released() []string {
	if len(l) == 1 {
		return l[0].operation.Bindings
	}
	return l.bindings()
}

func removalBinding(ctx context.Context, store recordReader, applied operationstore.Operation) (string, error) {
	if applied.Predecessor == "" {
		return firstBinding(applied.Bindings), nil
	}
	held, err := appliedLineage(ctx, store, applied)
	if err != nil {
		return "", err
	}
	return firstBinding(held[0].operation.Bindings), nil
}

func sourceBindings(ctx context.Context, store recordReader, source string) ([]string, error) {
	held, err := sourceLineage(ctx, store, source)
	if err != nil {
		return nil, err
	}
	return held.released(), nil
}

func withLineagePlan(ctx context.Context, store recordReader, decided transition, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (transition, error) {
	if decided.fresh || decided.verb != reconciliation.Apply || operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return decided, nil
	}
	merged, _, err := appliedView(ctx, store, operation, frozen, states)
	decided.lineagePlan = merged
	return decided, err
}

func (t transition) removed() reconciliation.Plan {
	if len(t.lineagePlan.Blocks) != 0 {
		return t.lineagePlan
	}
	return t.plan
}

func resolutionMembers(ctx context.Context, store recordReader, head lineageMember) ([]lineageMember, error) {
	if head.operation.Verb != reconciliation.Apply || head.operation.Predecessor == "" {
		return []lineageMember{head}, nil
	}
	held, err := lineageOf(ctx, store, head)
	if err != nil {
		return nil, err
	}
	slices.Reverse(held)
	return held, nil
}

func (l lineage) operationIDs() []string {
	ids := make([]string, 0, len(l))
	for _, member := range l {
		ids = append(ids, member.operation.ID)
	}
	return ids
}
