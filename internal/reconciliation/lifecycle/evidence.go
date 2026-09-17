package lifecycle

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// BlockEvidence is what one block of the current operation durably proved
// about one API object. It carries the capability's own evidence bytes
// unread: reconciliation never learns what a capability publishes, so a
// consumer decodes them through the capability that wrote them.
type BlockEvidence struct {
	Kind           string
	Object         string
	Implementation string
	Verb           reconciliation.Verb
	State          reconciliation.BlockState
	Evidence       json.RawMessage
}

// Evidence reports what the current operation's completed attempts proved
// about one exact object. It performs no probe and writes nothing, and an
// object no frozen block names has no entry: the absence of a record is not
// evidence that nothing was realized.
func (s Service) Evidence(ctx context.Context, contextName, kind, object string) ([]BlockEvidence, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if kind == "" || object == "" {
		return nil, failure("lifecycle.state", "an evidence read names no object", "")
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	var found []BlockEvidence
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil {
			return err
		}
		plan, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return err
		}
		for _, block := range plan.Blocks {
			if block.Kind != kind || block.Object != object {
				continue
			}
			evidence, err := blockEvidence(ctx, store, operation.ID, block, operation.Verb)
			if err != nil {
				return err
			}
			found = append(found, evidence)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// blockEvidence reads the last attempt a block durably completed. A block that
// never started, or whose last attempt is still running, proves nothing and
// carries no evidence rather than the bytes of an earlier attempt.
func blockEvidence(ctx context.Context, store OperationStore, operation string, block reconciliation.Block, verb reconciliation.Verb) (BlockEvidence, error) {
	evidence := BlockEvidence{
		Kind: block.Kind, Object: block.Object, Implementation: block.Implementation,
		Verb: verb, State: reconciliation.BlockPending,
	}
	record, err := store.Block(ctx, operation, block.ID)
	if err != nil {
		return BlockEvidence{}, err
	}
	evidence.State = record.State
	if record.Attempts < 1 {
		return evidence, nil
	}
	attempt, err := store.Attempt(ctx, operation, block.ID, record.Attempts)
	if err != nil {
		return BlockEvidence{}, err
	}
	if attempt.Phase == "observed" {
		evidence.Evidence = attempt.Evidence
	}
	return evidence, nil
}
