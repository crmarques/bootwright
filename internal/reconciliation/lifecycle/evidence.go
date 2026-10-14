package lifecycle

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// BlockEvidence carries durable facts decoded only by the evidence-owning capability.
type BlockEvidence struct {
	// ApplyProved populates Block with the detached original request and digest,
	// carrying references, never Secret material, for owner validation.
	Block          reconciliation.Block
	Kind           string
	Object         string
	Implementation string
	Verb           reconciliation.Verb
	State          reconciliation.BlockState
	Evidence       json.RawMessage
}

// Evidence reads current-operation facts without probing or writing.
// A missing record never proves that nothing was realized.
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

// provedDependencies reads direct dependencies in frozen plan order for apply.
// Removal dependencies prove absence, not prerequisites for an effect.
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

// applyDependencies uses the held transaction's store, never re-entering
// Workspace: removal may hold its exclusive lock while needing apply facts.
func applyDependencies(ctx context.Context, store recordReader, applyOperation, blockID string) ([]BlockEvidence, error) {
	operation, err := store.ReadOperation(ctx, applyOperation)
	if err != nil {
		return nil, err
	}
	if operation.Verb != reconciliation.Apply {
		return nil, failure("lifecycle.state", "dependency identity must come from the original apply", "")
	}
	plan, err := store.ReadPlan(ctx, applyOperation)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool { return block.ID == blockID })
	if index < 0 {
		return nil, failure("lifecycle.state", "the original apply has no dependency identity for this block", "")
	}
	dependencies := map[string]bool{}
	pending := slices.Clone(plan.Blocks[index].Dependencies)
	for len(pending) != 0 {
		id := pending[0]
		pending = pending[1:]
		if dependencies[id] {
			continue
		}
		dependencies[id] = true
		dependency := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool { return block.ID == id })
		if dependency < 0 {
			return nil, failure("lifecycle.state", "the original apply names a missing dependency", "")
		}
		pending = append(pending, plan.Blocks[dependency].Dependencies...)
	}
	var proved []BlockEvidence
	for _, dependency := range plan.Blocks {
		if !dependencies[dependency.ID] {
			continue
		}
		evidence, err := blockEvidence(ctx, store, applyOperation, dependency, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		evidence.Block, _ = plan.Block(dependency.ID)
		evidence.Evidence = slices.Clone(evidence.Evidence)
		proved = append(proved, evidence)
	}
	return proved, nil
}

// blockEvidence reads the latest attempt's last resolution, or the attempt
// itself. Unstarted or running records carry no evidence from earlier records.
func blockEvidence(ctx context.Context, store recordReader, operation string, block reconciliation.Block, verb reconciliation.Verb) (BlockEvidence, error) {
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
