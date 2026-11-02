package lifecycle

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// transition is the single legal next step the durable state permits. A noop
// transition is the legal step that has nothing left to do: the context
// already holds the state its verb would leave it in.
type operationPlan struct {
	fresh bool
	noop  bool
	// finalize marks an operation whose block records prove it complete while
	// its record, evidence, reservations or Secret bindings do not yet say so.
	finalize bool
	// unclaimed marks a destroy of a context holding no operation whose
	// running evidence or reservations an interrupted registration left.
	unclaimed bool
	// idle marks a destroy that settles beside a claim that holds nothing,
	// which only a transaction of its own reclaims.
	idle      bool
	verb      reconciliation.Verb
	operation operationstore.Operation
	plan      reconciliation.Plan
	binding   capabilityBinding
	source    string
	// reopen is the binding a fresh removal inherits: the one its apply froze,
	// so a removal presents the material that created what it removes rather
	// than whatever the current declarations name.
	reopen    string
	release   []string
	rebinds   []string
	rebound   rebinding
	states    map[string]reconciliation.BlockState
	selection reconciliation.Selection
	// phases are the removal phases of plan, decided once for every destroy;
	// replacedPhases are those of the failed removal a fresh removal replaces,
	// whose own plan its resolution observes.
	phases, replacedPhases removalPhases
	sameBlocks             sameBlocksProof
	lineagePlan            reconciliation.Plan
	predecessor            string
	takes                  []string
	// basis is the durable state this transition was planned from. For a fresh
	// removal it is the incomplete operation the removal takes the place of,
	// which is the context's current operation. It is not always source: a
	// removal that replaces a failed removal inherits that one's apply as its
	// source while replacing the removal itself.
	basis basis
}

// basis is the durable state a transition was planned from: the context's
// current operation, its whole record, and each of its frozen blocks' state and
// attempt count, or no operation at all. The frozen plan is part of it through
// the record's plan digest, which every read of the plan is held to. The
// decision is taken under the shared lock and the effects run under the
// exclusive one, so the transition re-proves this before it does anything.
//
// The apply a removal releases is not part of it. That apply is either the
// basis operation itself, for a fresh removal over an apply, or one that
// stopped being current when its first removal registered, and it is written
// only by the restoration and the resolutions recorded in the transaction that
// registers that removal, so nothing moves it between a later decision and its
// effects.
type basis struct {
	operation string
	state     reconciliation.OperationState
	record    operationstore.Operation
	blocks    map[string]reconciliation.BlockState
	attempts  map[string]int
	// revision and input are the input revision and digest a fresh apply
	// compiled its plan from. Every other transition plans from frozen records
	// rather than from the input, so it leaves them empty.
	revision string
	input    string
	// evidence is the mutation evidence the transition requires the context
	// to still hold: the running evidence a fresh apply raised, or what a
	// destroy of a context holding no operation decided to release.
	evidence []byte
	// claimed is the operation directory a fresh apply claimed, and claims the
	// operation directories right after that claim. A reclaim removes one only
	// while the evidence reads pristine, or once a registration moved the
	// index, which refuses this apply on its own, so a claim that raised the
	// evidence again after a release lowered it is still listed, even under
	// evidence that reads the same bytes again. The set is compared rather
	// than its size, because a reclaim of an older claim and that newer claim
	// leave the same number.
	claimed string
	claims  []string
	// context names the context the decision read, so a refusal that finds
	// this basis moved names the command that decides again in that context.
	context string
}

// describe names the operation a basis holds and that operation's state, in
// the words a refusal reports them with.
func (b basis) describe() string {
	if b.operation == "" {
		return "no operation"
	}
	return "operation " + b.operation + " (" + string(b.state) + ")"
}

func (p operationPlan) starting() []reconciliation.Block {
	var starting []reconciliation.Block
	for _, block := range p.plan.Blocks {
		current := p.states[block.ID]
		if current != reconciliation.BlockDone && (unproved(current) || p.selection.Admits(block)) {
			starting = append(starting, block)
		}
	}
	return starting
}
