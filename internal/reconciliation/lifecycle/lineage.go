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
	effects   map[string]reconciliation.EffectState
	holds     map[string]bool
	takenBack []string
	whole     bool
}

type lineage []lineageMember

func lineageOf(ctx context.Context, store recordReader, head lineageMember) (lineage, error) {
	if head.operation.Verb != reconciliation.Apply {
		return nil, engineRecordsDamaged.Fail("the operation "+head.operation.ID+" heads a lineage and is no apply", "restore the context store")
	}
	chain := lineage{head}
	seen := map[string]bool{head.operation.ID: true}
	restores := head.operation.Restores
	for id := head.operation.Predecessor; id != ""; {
		switch {
		case seen[id]:
			return nil, engineRecordsDamaged.Fail("the applies this operation adds to name one another in a cycle", "restore the context store")
		case len(seen) >= operationstore.MaxOperations:
			return nil, engineRecordsDamaged.Fail("the applies this operation adds to exceed the operations a context retains", "restore the context store")
		}
		seen[id] = true
		if restores != "" {
			restored, err := restoredMember(ctx, store, restores, id)
			if err != nil {
				return nil, err
			}
			chain = append(chain, restored)
		}
		record, err := store.ReadOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		if record.Verb != reconciliation.Apply {
			return nil, engineRecordsDamaged.Fail("the operation this apply adds to is no apply", "restore the context store")
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
		id, restores = record.Predecessor, record.Restores
	}
	slices.Reverse(chain)
	return chain, nil
}

func restoredMember(ctx context.Context, store recordReader, id, predecessor string) (lineageMember, error) {
	record, err := store.ReadOperation(ctx, id)
	if err != nil {
		return lineageMember{}, err
	}
	if !trimmed(record) || record.Source != predecessor {
		return lineageMember{}, engineRecordsDamaged.Fail("the operation the apply after "+predecessor+" restores is no completed staged destroy of it", "restore the context store")
	}
	return lineageMember{operation: record, plan: reconciliation.Plan{Verb: reconciliation.Destroy}, states: map[string]reconciliation.BlockState{}, takenBack: slices.Clone(record.Stage.TakenBack)}, nil
}

func (l lineage) applies() lineage {
	return slices.DeleteFunc(slices.Clone(l), func(member lineageMember) bool { return member.operation.Verb != reconciliation.Apply })
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

func (l lineage) members() []reconciliation.Member {
	members := make([]reconciliation.Member, 0, len(l))
	for _, member := range l {
		members = append(members, reconciliation.Member{
			Operation: member.operation.ID, Plan: member.plan, States: member.states, Effects: member.effects,
			Completed: member.operation.Verb == reconciliation.Apply && member.operation.State == reconciliation.OperationDone,
			TakenBack: member.takenBack, Whole: member.whole,
		})
	}
	return members
}

func (l lineage) ownership() reconciliation.Ownership { return reconciliation.Fold(l.members()...) }

type owners struct {
	chain  lineage
	folded reconciliation.Ownership
}

func (l lineage) owners() owners { return owners{chain: l, folded: l.ownership()} }

func (o owners) of(block string) (lineageMember, reconciliation.Block, bool) {
	holding, held := o.folded.Holding(block)
	if !held || holding.Incarnation < 0 || o.folded.TakenBack(holding) {
		return lineageMember{}, reconciliation.Block{}, false
	}
	return o.chain[holding.Incarnation], holding.Created, true
}

func (o owners) routed(block string) (lineageMember, error) {
	switch len(o.chain) {
	case 0:
		return lineageMember{}, engineRecordsDamaged.Fail("the operation names no apply to route the block "+block+" to", "restore the context store")
	case 1:
		return o.chain[0], nil
	}
	owner, _, held := o.of(block)
	if !held {
		return lineageMember{}, engineRecordsDamaged.Fail("no apply of the lineage of "+o.chain.head().operation.ID+" holds the block "+block, "restore the context store")
	}
	return owner, nil
}

func (o owners) owned() []OwnedBlock {
	var held []OwnedBlock
	for _, holding := range o.folded.Holdings() {
		if holding.Incarnation >= 0 && !o.folded.TakenBack(holding) {
			held = append(held, OwnedBlock{Operation: o.chain[holding.Incarnation].operation.ID, Block: holding.Created})
		}
	}
	return held
}

func (o owners) incarnated() []reconciliation.Plan {
	plans := make([]reconciliation.Plan, len(o.chain))
	for _, holding := range o.folded.Holdings() {
		if holding.Incarnation >= 0 && !o.folded.TakenBack(holding) {
			plans[holding.Incarnation].Blocks = append(plans[holding.Incarnation].Blocks, holding.Created)
		}
	}
	return plans
}

func (o owners) definitions() []reconciliation.BlockDefinition {
	var all []reconciliation.BlockDefinition
	for _, held := range o.owned() {
		all = append(all, held.Block.BlockDefinition)
	}
	return all
}

func (o owners) lending(plan reconciliation.Plan) []reconciliation.BlockDefinition {
	return o.lendingBeside(plan, nil)
}

func (o owners) lendingBeside(plan reconciliation.Plan, taken []string) []reconciliation.BlockDefinition {
	definitions := planDefinitions(plan)
	for _, definition := range o.definitions() {
		if _, held := plan.Block(definition.ID); !held && !slices.Contains(taken, definition.ID) {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

func (o owners) removalLending(plan reconciliation.Plan, stage *operationstore.Stage) ([]reconciliation.BlockDefinition, map[string]bool) {
	removing := map[string]bool{}
	for _, block := range plan.Blocks {
		removing[block.ID] = true
	}
	if stage == nil {
		return planDefinitions(plan), removing
	}
	return o.lendingBeside(plan, stage.TakenBack), removing
}

func (l lineage) ownerOf(block string) (lineageMember, bool) {
	owner, _, held := l.owners().of(block)
	return owner, held
}

func (l lineage) removalPlan(started reconciliation.Plan) (reconciliation.Plan, error) {
	return reconciliation.Fold(append(l[:len(l)-1].members(), reconciliation.Member{Plan: started, Completed: true})...).CoveredPlan()
}

func (l lineage) ownedPlan() (reconciliation.Plan, error) { return l.ownership().CoveredPlan() }

func (l lineage) merged(added reconciliation.Plan) (reconciliation.Plan, error) {
	return reconciliation.LineageOf(append(l.members(), reconciliation.Member{Plan: added})...)
}

// record is a member's own record, never what the lineage holds.
func (m lineageMember) record() (reconciliation.Plan, map[string]reconciliation.BlockState) {
	return m.plan, m.states
}

func (m lineageMember) unproved(abandoned map[string]bool) []string {
	return slices.DeleteFunc(unprovedBlocks(m.plan, m.states), func(id string) bool {
		return abandoned[id] || m.holds != nil && !m.holds[id]
	})
}

func (l lineage) plan() (reconciliation.Plan, error) {
	if len(l) == 1 {
		return l[0].plan, nil
	}
	return l.ownership().Plan()
}

func (l lineage) states() map[string]reconciliation.BlockState {
	if len(l) == 1 {
		return l[0].states
	}
	held := map[string]reconciliation.BlockState{}
	for _, holding := range l.ownership().Holdings() {
		if state, recorded := l[holding.Holder].states[holding.Block.ID]; recorded {
			held[holding.Block.ID] = state
		}
	}
	return held
}

func (l lineage) bindings() []string {
	var held []string
	for _, member := range l {
		held = joinBindings(held, member.operation.Bindings...)
	}
	return held
}

func (l lineage) definitions() []reconciliation.BlockDefinition { return l.owners().definitions() }

func (l lineage) routed(block string) (lineageMember, error) { return l.owners().routed(block) }

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
	for _, member := range chain.applies() {
		entries = append(entries, LineageEntry{Operation: member.operation.ID, Verb: string(member.operation.Verb), State: string(member.operation.State)})
	}
	return entries, nil
}

func (l lineage) owned() []OwnedBlock { return l.owners().owned() }

func (l lineage) lending(plan reconciliation.Plan) []reconciliation.BlockDefinition {
	return l.owners().lending(plan)
}

func (l lineage) removalLending(plan reconciliation.Plan, stage *operationstore.Stage) ([]reconciliation.BlockDefinition, map[string]bool) {
	return l.owners().removalLending(plan, stage)
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
	return engineRecordsDamaged.Fail("the operation this removal takes back is no apply", "restore the context store")
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
	for _, holding := range chain.ownership().Unfinished() {
		state := holding.State
		if state == "" {
			state = reconciliation.BlockPending
		}
		unfinished = append(unfinished, holding.Block.ID+" ("+string(state)+")")
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

// ownedLineage is what the index operation's ownership folds: an apply's
// lineage, or a removal over the apply it removes, which also took back what
// its source covered and an earlier attempt removed from its own plan.
func ownedLineage(ctx context.Context, store recordReader, current lineageMember) (lineage, error) {
	switch operation := current.operation; {
	case operation.Verb == reconciliation.Apply && operation.Predecessor != "":
		held, err := lineageOf(ctx, store, current)
		if err != nil {
			return nil, err
		}
		_, err = held.plan()
		return held, err
	case operation.Verb == reconciliation.Destroy && operation.Source != "":
		return removalLineage(ctx, store, current)
	}
	return lineage{current}, nil
}

// removalLineage folds the removed apply lineage, the removals it replaces by
// `replaces`, oldest first, and itself.
func removalLineage(ctx context.Context, store recordReader, current lineageMember) (lineage, error) {
	held, err := statedLineage(ctx, store, current.operation.Source)
	if err != nil {
		return nil, err
	}
	var covered []string
	for _, holding := range held.ownership().Covered() {
		covered = append(covered, holding.Block.ID)
	}
	removals, err := replacedChain(ctx, store, current.operation)
	if err != nil {
		return nil, err
	}
	removals = append(removals, current)
	for index := range removals {
		member := &removals[index]
		if member.states == nil {
			if member.states, err = store.BlockStates(ctx, member.operation.ID, member.plan); err != nil {
				return nil, err
			}
		}
		if member.effects, err = settledEffects(ctx, store, member.operation.ID, member.plan, member.states); err != nil {
			return nil, err
		}
		member.whole = member.operation.Stage == nil
		took := covered
		if !member.whole {
			took = member.operation.Stage.TakenBack
		}
		for _, id := range took {
			if _, removing := member.plan.Block(id); !removing {
				member.takenBack = append(member.takenBack, id)
			}
		}
	}
	return append(held, removals...), nil
}

func statedLineage(ctx context.Context, store recordReader, source string) (lineage, error) {
	applied, err := store.ReadOperation(ctx, source)
	if err != nil {
		return nil, err
	}
	if applied.Verb != reconciliation.Apply {
		return nil, notAnApply()
	}
	plan, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return nil, err
	}
	states, err := store.BlockStates(ctx, applied.ID, plan)
	if err != nil {
		return nil, err
	}
	return lineageOf(ctx, store, lineageMember{operation: applied, plan: plan, states: states})
}

func replacedChain(ctx context.Context, store recordReader, operation operationstore.Operation) (lineage, error) {
	var chain lineage
	seen := map[string]bool{operation.ID: true}
	for id := operation.Replaces; id != ""; {
		if seen[id] || len(seen) > operationstore.MaxOperations {
			return nil, engineRecordsDamaged.Fail("the removals this operation replaces name one another in a cycle or exceed the operations a context retains", "restore the context store")
		}
		seen[id] = true
		record, err := store.ReadOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		if record.Verb != reconciliation.Destroy || record.Source != operation.Source {
			return nil, engineRecordsDamaged.Fail("the operation this removal replaces is no removal of the same apply", "restore the context store")
		}
		plan, err := store.ReadPlan(ctx, id)
		if err != nil {
			return nil, err
		}
		chain = append(chain, lineageMember{operation: record, plan: plan})
		id = record.Replaces
	}
	slices.Reverse(chain)
	return chain, nil
}

func (l lineage) kept() map[string]bool {
	kept := map[string]bool{}
	for _, holding := range l.ownership().Holdings() {
		if l[holding.Holder].operation.Verb == reconciliation.Apply && holding.Incarnation >= 0 {
			kept[holding.Block.ID] = true
		}
	}
	return kept
}

func (l lineage) live() ([]string, map[string]reconciliation.BlockState) {
	var live []string
	states := map[string]reconciliation.BlockState{}
	folded := l.ownership()
	for _, holding := range folded.Covered() {
		if holding.Incarnation >= 0 && !folded.TakenBack(holding) {
			live = append(live, holding.Block.ID)
			states[holding.Block.ID] = holding.State
		}
	}
	return live, states
}

func (l lineage) unstarted() []string {
	folded := l.ownership()
	covered := map[string]bool{}
	for _, holding := range folded.Covered() {
		covered[holding.Block.ID] = true
	}
	var ids []string
	for _, holding := range folded.Holdings() {
		if holding.Incarnation >= 0 && !folded.TakenBack(holding) && !covered[holding.Block.ID] {
			ids = append(ids, holding.Block.ID)
		}
	}
	return ids
}

func (l lineage) replacedReach() []map[string][]reconciliation.BlockState {
	var reached []map[string][]reconciliation.BlockState
	for index := len(l) - 2; index >= 0 && l[index].operation.Verb == reconciliation.Destroy; index-- {
		reached = append(reached, reachedByObject(l[index].plan, l[index].states))
	}
	return reached
}

func (l lineage) remaining(frozen reconciliation.Plan) reconciliation.Plan {
	folded, last := l.ownership(), len(l)-1
	return reconciliation.Plan{Verb: frozen.Verb, Blocks: slices.DeleteFunc(slices.Clone(frozen.Blocks), func(block reconciliation.Block) bool {
		holding, held := folded.Holding(block.ID)
		return !held || holding.Holder != last || holding.State == reconciliation.BlockDone
	})}
}

func (l lineage) current(holding reconciliation.Holding) bool {
	member := l[holding.Holder]
	if member.operation.Verb == reconciliation.Apply {
		return true
	}
	_, own := member.plan.Block(holding.Block.ID)
	return own && holding.Holder == len(l)-1
}

func settledEffects(ctx context.Context, store recordReader, operation string, plan reconciliation.Plan, states map[string]reconciliation.BlockState) (map[string]reconciliation.EffectState, error) {
	effects := map[string]reconciliation.EffectState{}
	for _, block := range plan.Blocks {
		if state := states[block.ID]; state != reconciliation.BlockDone && state != reconciliation.BlockFailed {
			continue
		}
		record, err := store.Block(ctx, operation, block.ID)
		if err != nil {
			return nil, err
		}
		if record.Attempts < 1 {
			continue
		}
		settled, resolved, err := store.LastResolution(ctx, operation, block.ID, record.Attempts)
		if err != nil {
			return nil, err
		}
		if !resolved || settled.Effect == "" {
			if settled, err = store.Attempt(ctx, operation, block.ID, record.Attempts); err != nil {
				return nil, err
			}
		}
		effects[block.ID] = settled.Effect
	}
	return effects, nil
}

// coveredBy is what a removal of an operation takes back, and so what every view
// of its ownership reports: a completed apply's whole plan, an incomplete
// apply's started blocks, or a removal's blocks not yet proved gone.
func coveredBy(operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) reconciliation.Plan {
	covered := reconciliation.Plan{Verb: plan.Verb, Blocks: []reconciliation.Block{}, Preparation: plan.Preparation}
	for _, holding := range (lineage{{operation: operation, plan: plan, states: states}}).ownership().Covered() {
		covered.Blocks = append(covered.Blocks, holding.Block)
	}
	return covered
}
