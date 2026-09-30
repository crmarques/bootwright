package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// unexplained is what an observation that proved nothing says when its
// capability cannot say more from what it recorded.
var unexplained = Unresolved{
	Reason: "its observation proved neither the effect nor its absence",
	Remedy: "restore the target it ran against so an observation can read it",
}

// unobserved is what an unproved block says before any resolution observed
// it: an attempt whose outcome was lost, or whose executor died, is observed
// by the next invocation of either verb before anything else starts.
var unobserved = Unresolved{
	Reason: "its attempt's outcome was not recorded and no observation has read it yet",
	Remedy: "repeat the verb, which observes it before anything else starts",
}

// explain says why one block's observation proved nothing, from the evidence
// that observation recorded. The capability that froze the block names the
// reason when it can, and otherwise the engine gives the one every such
// observation shares. Evidence an observation did not return reaches the
// capability empty, whether it was never recorded or was recorded as null, so
// the refusal after an observation and a later status read the same thing.
func (s Service) explain(block reconciliation.Block, evidence json.RawMessage) Unresolved {
	if bytes.Equal(bytes.TrimSpace(evidence), []byte("null")) {
		evidence = nil
	}
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return unexplained
	}
	reporter, ok := capability.(UnresolvedReporter)
	if !ok {
		return unexplained
	}
	if unresolved, ok := reporter.Unresolved(block, evidence); ok && unresolved.Reason != "" && unresolved.Remedy != "" {
		return unresolved
	}
	return unexplained
}

// unresolvedFailure is the diagnostic of one effect an observation left
// unknown. It names the block, why it stayed unknown and what the operator
// does before repeating the verb, which then observes it again.
func unresolvedFailure(block string, unresolved Unresolved) error {
	return failure("lifecycle.unknown",
		"the outcome of "+block+" is still unknown: "+unresolved.Reason,
		unresolved.Remedy+", then repeat the operation to observe it again")
}

// unresolvedOf reads why one unproved block of an operation is still unknown:
// what the last resolution of its last attempt recorded, when one observed it.
// Each resolution supersedes the one before it, so that record is the one that
// left the block unknown.
func (s Service) unresolvedOf(ctx context.Context, store OperationStore, operation string, block reconciliation.Block) (Unresolved, error) {
	record, err := store.Block(ctx, operation, block.ID)
	if err != nil {
		return Unresolved{}, err
	}
	if record.Attempts < 1 {
		return unobserved, nil
	}
	resolution, resolved, err := store.LastResolution(ctx, operation, block.ID, record.Attempts)
	if err != nil {
		return Unresolved{}, err
	}
	if !resolved || resolution.Phase != "observed" {
		return unobserved, nil
	}
	return s.explain(block, resolution.Evidence), nil
}

// explainUnproved names, for each block of a result that is unknown or still
// running, why its outcome is unproved.
func (s Service) explainUnproved(ctx context.Context, store OperationStore, operation string, plan reconciliation.Plan, blocks []BlockResult) error {
	for index := range blocks {
		if !unproved(reconciliation.BlockState(blocks[index].State)) {
			continue
		}
		block, ok := plan.Block(blocks[index].ID)
		if !ok {
			continue
		}
		unresolved, err := s.unresolvedOf(ctx, store, operation, block)
		if err != nil {
			return err
		}
		blocks[index].Unresolved = &unresolved
	}
	return nil
}
