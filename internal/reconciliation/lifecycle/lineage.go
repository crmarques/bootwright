package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type lineageMember struct {
	operation operationstore.Operation
	plan      reconciliation.Plan
	states    map[string]reconciliation.BlockState
}

type lineage []lineageMember

func lineageOf(ctx context.Context, store recordReader, head lineageMember) (lineage, error) {
	if head.operation.Verb != reconciliation.Apply {
		return nil, failure("lifecycle.state", "the operation "+head.operation.ID+" heads a lineage and is no apply", "restore the context store")
	}
	chain := lineage{head}
	seen := map[string]bool{head.operation.ID: true}
	for id := head.operation.Predecessor; id != ""; {
		switch {
		case seen[id]:
			return nil, failure("lifecycle.state", "the applies this operation adds to name one another in a cycle", "restore the context store")
		case len(seen) >= operationstore.MaxOperations:
			return nil, failure("lifecycle.state", "the applies this operation adds to exceed the operations a context retains", "restore the context store")
		}
		seen[id] = true
		record, err := store.ReadOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		if record.Verb != reconciliation.Apply {
			return nil, failure("lifecycle.state", "the operation this apply adds to is no apply", "restore the context store")
		}
		plan, err := store.ReadPlan(ctx, id)
		if err != nil {
			return nil, err
		}
		states, err := store.BlockStates(ctx, id, plan)
		if err != nil {
			return nil, err
		}
		chain = append(chain, lineageMember{operation: record, plan: plan, states: states})
		id = record.Predecessor
	}
	slices.Reverse(chain)
	return chain, nil
}

func appliedLineage(ctx context.Context, store recordReader, operation operationstore.Operation) (lineage, error) {
	applied := operation
	if operation.Verb != reconciliation.Apply {
		if operation.Source == "" {
			return nil, nil
		}
		var err error
		if applied, err = store.ReadOperation(ctx, operation.Source); err != nil {
			return nil, err
		}
		if applied.Verb != reconciliation.Apply {
			return nil, notAnApply()
		}
	}
	plan, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return nil, err
	}
	return lineageOf(ctx, store, lineageMember{operation: applied, plan: plan})
}

func (l lineage) head() lineageMember { return l[len(l)-1] }

func (l lineage) ownerOf(block string) (lineageMember, bool) {
	index := slices.IndexFunc(l, func(member lineageMember) bool {
		_, held := member.plan.Block(block)
		return held
	})
	if index < 0 {
		return lineageMember{}, false
	}
	return l[index], true
}

func (l lineage) plan() (reconciliation.Plan, error) {
	if len(l) == 1 {
		return l[0].plan, nil
	}
	plans := make([]reconciliation.Plan, 0, len(l))
	for _, member := range l {
		plans = append(plans, member.plan)
	}
	return reconciliation.Lineage(plans...)
}

func (l lineage) states() map[string]reconciliation.BlockState {
	if len(l) == 1 {
		return l[0].states
	}
	union := map[string]reconciliation.BlockState{}
	for _, member := range l {
		for id, state := range member.states {
			union[id] = state
		}
	}
	return union
}

func (l lineage) bindings() []string {
	var held []string
	for _, member := range l {
		held = joinBindings(held, member.operation.Bindings...)
	}
	return held
}

func (l lineage) definitions() []reconciliation.BlockDefinition {
	var all []reconciliation.BlockDefinition
	for _, member := range l {
		all = append(all, planDefinitions(member.plan)...)
	}
	return all
}

func (l lineage) routed(block string) (lineageMember, error) {
	switch len(l) {
	case 0:
		return lineageMember{}, failure("lifecycle.state", "the operation names no apply to route the block "+block+" to", "restore the context store")
	case 1:
		return l[0], nil
	}
	owner, held := l.ownerOf(block)
	if !held {
		return lineageMember{}, failure("lifecycle.state", "no apply of the lineage of "+l.head().operation.ID+" holds the block "+block, "restore the context store")
	}
	return owner, nil
}

func (l lineage) ownerFor(block string) (string, error) {
	if len(l) == 0 {
		return "", nil
	}
	owner, err := l.routed(block)
	return owner.operation.ID, err
}

func lineageEntries(ctx context.Context, store recordReader, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) ([]LineageEntry, error) {
	if operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return nil, nil
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: plan, states: states})
	if err != nil {
		return nil, err
	}
	entries := make([]LineageEntry, 0, len(chain))
	for _, member := range chain {
		entries = append(entries, LineageEntry{Operation: member.operation.ID, Verb: string(member.operation.Verb), State: string(member.operation.State)})
	}
	return entries, nil
}

func (l lineage) owned() []OwnedBlock {
	var held []OwnedBlock
	for _, member := range l {
		for _, block := range member.plan.Blocks {
			held = append(held, OwnedBlock{Operation: member.operation.ID, Block: block})
		}
	}
	return held
}

func (l lineage) lending(plan reconciliation.Plan) []reconciliation.BlockDefinition {
	definitions := planDefinitions(plan)
	if len(l) < 2 {
		return definitions
	}
	for _, definition := range l.definitions() {
		if _, held := plan.Block(definition.ID); !held {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

func ownerID(ctx context.Context, store recordReader, operation operationstore.Operation, block string) (string, error) {
	if operation.Verb == reconciliation.Apply {
		return ownerIn(ctx, store, operation, block)
	}
	if operation.Source == "" {
		return "", nil
	}
	applied, err := store.ReadOperation(ctx, operation.Source)
	if err != nil {
		return "", err
	}
	if applied.Verb != reconciliation.Apply {
		return "", notAnApply()
	}
	return ownerIn(ctx, store, applied, block)
}

func ownerIn(ctx context.Context, store recordReader, applied operationstore.Operation, block string) (string, error) {
	owner, err := routedIn(ctx, store, applied, block)
	return owner.ID, err
}

func routedIn(ctx context.Context, store recordReader, applied operationstore.Operation, block string) (operationstore.Operation, error) {
	if applied.Predecessor == "" {
		return applied, nil
	}
	chain, err := appliedLineage(ctx, store, applied)
	if err != nil {
		return operationstore.Operation{}, err
	}
	owner, err := chain.routed(block)
	return owner.operation, err
}

func predecessorLineage(ctx context.Context, store recordReader, operation operationstore.Operation) (lineage, error) {
	if operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return nil, nil
	}
	return appliedLineage(ctx, store, operation)
}

func sourceLineage(ctx context.Context, store recordReader, source string) (lineage, error) {
	applied, err := store.ReadOperation(ctx, source)
	if err != nil {
		return nil, err
	}
	if applied.Verb != reconciliation.Apply {
		return nil, notAnApply()
	}
	return appliedLineage(ctx, store, applied)
}

func notAnApply() error {
	return failure("lifecycle.state", "the operation this removal takes back is no apply", "restore the context store")
}

func appliedView(ctx context.Context, store recordReader, applied operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) (reconciliation.Plan, map[string]reconciliation.BlockState, error) {
	if applied.Predecessor == "" {
		return plan, states, nil
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: applied, plan: plan, states: states})
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	merged, err := chain.plan()
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	return merged, chain.states(), nil
}

func unfinishedOver(ctx context.Context, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) ([]string, error) {
	if operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return unfinishedBlocks(frozen, states), nil
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return nil, err
	}
	unfinished := []string{}
	for _, member := range chain {
		unfinished = append(unfinished, unfinishedBlocks(member.plan, member.states)...)
	}
	return unfinished, nil
}

func lineageBindings(ctx context.Context, store recordReader, id string) ([]string, error) {
	head, err := store.ReadOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if head.Predecessor == "" {
		return nil, nil
	}
	plan, err := store.ReadPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: head, plan: plan})
	if err != nil {
		return nil, err
	}
	return chain.bindings(), nil
}
