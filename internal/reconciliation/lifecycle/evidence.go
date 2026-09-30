package lifecycle

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
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

// Evidence reports what the current operation's completed attempts and
// resolutions proved about one exact object. It performs no probe and writes
// nothing, and an object no frozen block names has no entry: the absence of a
// record is not evidence that nothing was realized.
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

// provedDependencies reads what every block one block depends on durably
// proved in this operation, in frozen plan order. Only an apply attempt
// receives it: a removal's dependencies are the blocks it removes before, and
// they prove absence rather than anything its effect relies on.
func provedDependencies(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block) ([]BlockEvidence, error) {
	if operation.Verb != reconciliation.Apply {
		return nil, nil
	}
	var proved []BlockEvidence
	for _, dependency := range plan.Blocks {
		if !slices.Contains(block.Dependencies, dependency.ID) {
			continue
		}
		evidence, err := blockEvidence(ctx, store, operation.ID, dependency, operation.Verb)
		if err != nil {
			return nil, err
		}
		proved = append(proved, evidence)
	}
	return proved, nil
}

// blockEvidence reads the record that last settled a block: its last attempt,
// or the last resolution of that attempt when one observed it, since only an
// unproved attempt is resolved and what the resolution observed is then what
// the block proved. A block that never started, or whose settling record is
// still running, proves nothing and carries no evidence rather than the bytes
// of an earlier record.
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
	settled, err := store.Attempt(ctx, operation, block.ID, record.Attempts)
	if err != nil {
		return BlockEvidence{}, err
	}
	resolution, resolved, err := store.LastResolution(ctx, operation, block.ID, record.Attempts)
	if err != nil {
		return BlockEvidence{}, err
	}
	if resolved {
		settled = resolution
	}
	if settled.Phase == "observed" {
		evidence.Evidence = settled.Evidence
	}
	return evidence, nil
}
