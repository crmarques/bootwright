package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// freshDestroy plans removal from the plan the operation being removed froze,
// so a build whose derivation has moved since still removes exactly the effects
// that exist. It reads no desired state: the frozen requests are the effects,
// and this executable only reads them.
//
// An apply that started no block owns nothing, so its removal carries no block:
// it registers, performs nothing and completes, which releases what the apply
// claimed and leaves the context at rest rather than holding an apply that only
// the exact input it froze could ever continue. startedNothing says the caller
// proved the operation is such an apply (mayHaveStartedNothing). Anywhere else
// an empty set means the operation contradicts its own block records, and a
// removal of nothing would release the material the effects still on the host
// need, so it refuses.
func (s Service) freshDestroy(ctx context.Context, contextName string, frozenBy operationstore.Executable, source string, scope removalScope, startedNothing bool) (operationPlan, error) {
	if scope.started == 0 && !startedNothing {
		return operationPlan{}, failure("lifecycle.state",
			"the operation this removal supersedes records no block it still owns",
			reviewStatus(contextName))
	}
	plan, err := s.removalOf(ctx, scope.owned, frozenBy)
	if err != nil {
		return operationPlan{}, err
	}
	return operationPlan{
		fresh: true, verb: reconciliation.Destroy, plan: plan,
		source: source, reopen: scope.reopen, release: scope.release, takes: scope.operations,
	}, nil
}

// stopsFirst names the objects whose removal the quiescence gate refuses while
// they run, so the plan says what to stop before the operator confirms it.
func (s Service) stopsFirst(plan reconciliation.Plan, phases removalPhases) []string {
	var named []string
	for _, block := range plan.Blocks {
		if phases.machines[block.ID] || phases.preStopMachines[block.ID] {
			continue
		}
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !ok {
			continue
		}
		if prober, probes := capability.(QuiescenceProber); probes && prober.ProbesQuiescence() && !slices.Contains(named, block.Object) {
			named = append(named, block.Object)
		}
	}
	return named
}

// removalOf turns the frozen blocks an operation still owns into the plan that
// removes them. Every block keeps the identity, implementation, content digest
// and request its apply froze; its capability supplies only what removing it
// decides, so the plan describes the effects that exist rather than the effects
// this executable would create from the same input today.
func (s Service) removalOf(ctx context.Context, owned reconciliation.Plan, executable operationstore.Executable) (reconciliation.Plan, error) {
	inverted, err := invertOwned(owned)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	definitions := make([]reconciliation.BlockDefinition, 0, len(inverted.Blocks))
	for _, block := range inverted.Blocks {
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !ok {
			return reconciliation.Plan{}, failure("lifecycle.state",
				"this executable provides no "+block.Implementation+" to remove the block "+block.ID+" with",
				removeWith(executable))
		}
		removal, err := capability.Removal(ctx, block)
		if err != nil {
			return reconciliation.Plan{}, unreadable(block, executable, err)
		}
		definition := block.BlockDefinition
		definition.Dependencies = slices.Clone(definition.Dependencies)
		// Requirements are already resolved into the frozen dependencies this
		// removal inverted, and a subset may no longer contain the block a
		// requirement names.
		definition.Requires = nil
		definition.Description, definition.Impacts = removal.Description, removal.Impacts
		definition.Consumes, definition.Groups = removal.Consumes, removal.Groups
		definitions = append(definitions, definition)
	}
	return reconciliation.NewPlan(reconciliation.Destroy, definitions)
}

// invertOwned shapes the owned set as a removal. An apply's plan is inverted so
// every block waits on its own dependents; a removal's own plan already is that
// inverse and is only narrowed, which drops the edges to blocks it no longer
// carries.
func invertOwned(owned reconciliation.Plan) (reconciliation.Plan, error) {
	if owned.Verb == reconciliation.Apply {
		return owned.Inverse()
	}
	identities := make([]string, 0, len(owned.Blocks))
	for _, block := range owned.Blocks {
		identities = append(identities, block.ID)
	}
	return owned.Retain(identities)
}

// unreadable reports a frozen block this executable cannot read, naming what it
// holds and the executable that wrote it, so the remedy is a command rather
// than the obstacle that stopped it.
func unreadable(block reconciliation.Block, executable operationstore.Executable, err error) error {
	detail := ""
	if reported := diagnostics.Of(err); len(reported) != 0 {
		detail = ": " + reported[0].Message
	}
	return failure("lifecycle.state",
		"the request the block "+block.ID+" froze cannot be read by this executable"+detail,
		removeWith(executable))
}

// removeWith names the executable that registered what is being removed. An
// operation recorded before that identity existed names none, and the operator
// is pointed at the record instead.
func removeWith(executable operationstore.Executable) string {
	return "remove it with " + registeredWith(executable)
}
