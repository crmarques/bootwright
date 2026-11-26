package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// removalScope is what a fresh removal takes back, presents and releases.
type removalScope struct {
	owned      reconciliation.Plan
	started    int
	presents   presenting
	inherited  map[string]string
	release    []string
	operations []string
	contracts  map[string]string
	records    func(context.Context, string) (RemovalRecords, error)
	keptHosts  map[string][]string
	blanks     map[string][]string
}

func removalOver(ctx context.Context, store recordReader, applied operationstore.Operation, frozen, started reconciliation.Plan) (removalScope, error) {
	scope := removalScope{contracts: applied.Contracts, owned: started, started: len(started.Blocks), presents: presenting{lineage: firstBinding(applied.Bindings)}, release: applied.Bindings, records: recordsFrom(store, applied)}
	if applied.Predecessor == "" {
		return scope, nil
	}
	held, err := lineageOf(ctx, store, lineageMember{operation: applied, plan: frozen})
	if err != nil {
		return removalScope{}, err
	}
	scope.contracts = map[string]string{}
	for _, member := range held.applies() {
		for implementation, contract := range member.operation.Contracts {
			if previous, ok := scope.contracts[implementation]; ok && previous != contract {
				return removalScope{}, engineOtherBuild.Fail("the lineage holds conflicting execution contracts for "+implementation, removeWith(applied.Executable))
			}
			scope.contracts[implementation] = contract
		}
	}
	if scope.owned, err = held.removalPlan(started); err != nil {
		return removalScope{}, unremovable(applied, err)
	}
	scope.presents, scope.release = held.presenting(), held.bindings()
	scope.operations = held.applies().operationIDs()
	return scope, nil
}

func unremovable(applied operationstore.Operation, cause error) error {
	message := "the lineage of the apply " + applied.ID + " cannot be taken back as one removal"
	if reported := diagnostics.Of(cause); len(reported) != 0 {
		message += ": " + reported[0].Message
	}
	return engineRecordsDamaged.Fail(message, "restore the context store")
}

func replacementOf(ctx context.Context, store recordReader, removal operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (removalScope, error) {
	remaining := reconciliation.RemainingSubset(frozen, states)
	scope := removalScope{contracts: removal.Contracts, owned: remaining, started: len(remaining.Blocks), presents: presenting{lineage: firstBinding(removal.Bindings)}, release: slices.Clone(removal.Bindings), records: recordsFrom(store, removal)}
	if removal.Source == "" {
		return scope, nil
	}
	folded, err := removalLineage(ctx, store, lineageMember{operation: removal, plan: frozen, states: states})
	if err != nil {
		return removalScope{}, err
	}
	scope.owned = folded.remaining(frozen)
	scope.started = len(scope.owned.Blocks)
	held, err := sourceLineage(ctx, store, removal.Source)
	if err != nil {
		return removalScope{}, err
	}
	scope.release = joinBindings(scope.release, held.released()...)
	scope.operations = held.operationIDs()
	scope.presents, scope.inherited = folded.presenting(), folded.replacedRebounds()
	return scope, nil
}

func (l lineage) released() []string {
	if len(l) == 1 {
		return l[0].operation.Bindings
	}
	return l.bindings()
}

func removalBinding(ctx context.Context, store recordReader, applied operationstore.Operation) (string, error) {
	presents, err := appliedPresenting(ctx, store, applied)
	return presents.lineage, err
}

func sourceBindings(ctx context.Context, store recordReader, source string) ([]string, error) {
	held, err := sourceLineage(ctx, store, source)
	if err != nil {
		return nil, err
	}
	return held.released(), nil
}

func withLineagePlan(ctx context.Context, store recordReader, decided operationPlan, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (operationPlan, error) {
	if decided.fresh || decided.verb != reconciliation.Apply || operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return decided, nil
	}
	merged, _, err := appliedView(ctx, store, operation, frozen, states)
	decided.lineagePlan = merged
	return decided, err
}

func (t operationPlan) removed() reconciliation.Plan {
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
	for index := range held {
		held[index].holds = map[string]bool{}
	}
	for _, holding := range held.ownership().Holdings() {
		held[holding.Holder].holds[holding.Block.ID] = true
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

func readDeferredSource(ctx context.Context, store OperationStore, removal operationstore.Operation) (lineageMember, error) {
	applied, err := store.ReadOperation(ctx, removal.Source)
	if err != nil {
		return lineageMember{}, err
	}
	if applied.Verb != reconciliation.Apply {
		return lineageMember{}, notAnApply()
	}
	plan, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return lineageMember{}, err
	}
	states, err := store.BlockStates(ctx, applied.ID, plan)
	if err != nil {
		return lineageMember{}, err
	}
	damaged := func(id string) error {
		return engineRecordsDamaged.Fail("the original apply has no consistent deferred proof for "+id, "restore the context store")
	}
	for _, block := range unprovedBlocks(plan, states) {
		if _, held := slices.BinarySearch(removal.DeferredSourceProofs, block); !held {
			return lineageMember{}, damaged(block)
		}
	}
	for _, id := range removal.DeferredSourceProofs {
		record, err := store.Block(ctx, applied.ID, id)
		if err != nil {
			return lineageMember{}, err
		}
		if _, found := states[id]; !found || record.Attempts == 0 || record.State == reconciliation.BlockPending {
			return lineageMember{}, damaged(id)
		}
		if unproved(record.State) {
			continue
		}
		proof, found, err := store.LastResolution(ctx, applied.ID, id, record.Attempts)
		if err != nil {
			return lineageMember{}, err
		}
		_, state, transitionErr := reconciliation.ResolutionTransition(proof.Effect)
		if !found || proof.Phase != "observed" || transitionErr != nil || state != record.State ||
			proof.Effect == reconciliation.EffectInconclusive || proof.Failure != nil {
			return lineageMember{}, damaged(id)
		}
	}
	return lineageMember{operation: applied, plan: plan, states: states}, nil
}
