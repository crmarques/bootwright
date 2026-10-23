package lifecycle

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// RemovalEvidence pairs the same block's original apply and completed inverse.
type RemovalEvidence struct {
	Removed BlockEvidence
	Applied BlockEvidence
}

func removalRecords(ctx context.Context, store recordReader, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block) ([]RemovalEvidence, []json.RawMessage, error) {
	if operation.Verb != reconciliation.Destroy {
		return nil, nil, nil
	}
	preparations, err := attemptPreparations(ctx, store, operation.ID, block.ID)
	if err != nil {
		return nil, nil, err
	}
	original, err := store.ReadPlan(ctx, operation.Source)
	if err != nil {
		return nil, nil, err
	}
	wanted := map[string]bool{}
	pending := slices.Clone(block.Dependencies)
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		if wanted[id] {
			continue
		}
		dependency, found := plan.Block(id)
		if !found {
			return nil, nil, failure("lifecycle.state", "the removal names a missing dependency", "")
		}
		wanted[id] = true
		pending = append(pending, dependency.Dependencies...)
	}
	var proofs []RemovalEvidence
	for _, dependency := range plan.Blocks {
		if !wanted[dependency.ID] {
			continue
		}
		removed, err := blockEvidence(ctx, store, operation.ID, dependency, reconciliation.Destroy)
		if err != nil {
			return nil, nil, err
		}
		if removed.State != reconciliation.BlockDone || len(removed.Evidence) == 0 {
			continue
		}
		appliedBlock, found := original.Block(dependency.ID)
		if !found {
			return nil, nil, failure("lifecycle.state", "the removal dependency has no original apply identity", "")
		}
		applied, err := blockEvidence(ctx, store, operation.Source, appliedBlock, reconciliation.Apply)
		if err != nil {
			return nil, nil, err
		}
		removed.Block, _ = plan.Block(dependency.ID)
		applied.Block = appliedBlock
		removed.Evidence, applied.Evidence = slices.Clone(removed.Evidence), slices.Clone(applied.Evidence)
		proofs = append(proofs, RemovalEvidence{Removed: removed, Applied: applied})
	}
	return proofs, preparations, nil
}

func attemptPreparations(ctx context.Context, store recordReader, operation, block string) ([]json.RawMessage, error) {
	record, err := store.Block(ctx, operation, block)
	if err != nil {
		return nil, err
	}
	var preparations []json.RawMessage
	for number := 1; number <= record.Attempts; number++ {
		attempt, err := store.Attempt(ctx, operation, block, number)
		if err != nil {
			return nil, err
		}
		if len(attempt.Preparation) > 0 {
			preparations = append(preparations, slices.Clone(attempt.Preparation))
		}
	}
	return preparations, nil
}

func originalRemovalDependents(ctx context.Context, store recordReader, operation operationstore.Operation, block string) ([]BlockEvidence, error) {
	if operation.Verb != reconciliation.Destroy {
		return nil, nil
	}
	original, err := store.ReadPlan(ctx, operation.Source)
	if err != nil {
		return nil, err
	}
	if _, found := original.Block(block); !found {
		return nil, failure("lifecycle.state", "the removal block has no original apply identity", "")
	}
	wanted := map[string]bool{block: true}
	for changed := true; changed; {
		changed = false
		for _, candidate := range original.Blocks {
			if wanted[candidate.ID] {
				continue
			}
			for _, dependency := range candidate.Dependencies {
				if wanted[dependency] {
					wanted[candidate.ID], changed = true, true
					break
				}
			}
		}
	}
	var proved []BlockEvidence
	for _, candidate := range original.Blocks {
		if candidate.ID == block || !wanted[candidate.ID] {
			continue
		}
		proof, err := blockEvidence(ctx, store, operation.Source, candidate, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		proof.Block, _ = original.Block(candidate.ID)
		proof.Evidence = slices.Clone(proof.Evidence)
		proved = append(proved, proof)
	}
	return proved, nil
}

func populateRemovalExecution(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, execution *Execution) error {
	proved, preparations, err := removalRecords(ctx, store, operation, plan, execution.Block)
	if err != nil {
		return err
	}
	dependents, err := originalRemovalDependents(ctx, store, operation, execution.Block.ID)
	if err != nil {
		return err
	}
	execution.RemovalProved, execution.RemovalPreparations = proved, preparations
	execution.OriginalRemovalDependents = dependents
	if operation.Verb == reconciliation.Destroy && execution.Resolution == 0 && execution.Attempt > 0 {
		execution.PrepareRemoval = func(inner context.Context, preparation json.RawMessage) error {
			return store.RecordPreparation(inner, operation.ID, execution.Block.ID, execution.Attempt, slices.Clone(preparation))
		}
	}
	return nil
}
