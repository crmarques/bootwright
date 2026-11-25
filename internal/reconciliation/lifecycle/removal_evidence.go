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

func removalRecords(ctx context.Context, store recordReader, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block, carry bool) ([]RemovalEvidence, []json.RawMessage, error) {
	if operation.Verb != reconciliation.Destroy {
		return nil, nil, nil
	}
	preparations, err := attemptPreparations(ctx, store, operation.ID, block.ID)
	if err != nil {
		return nil, nil, err
	}
	held, err := sourceLineage(ctx, store, operation.Source)
	if err != nil {
		return nil, nil, err
	}
	wanted, err := removalClosure(plan, block.Dependencies)
	if err != nil {
		return nil, nil, err
	}
	proofs, err := provedRemovals(ctx, store, held, operation.ID, plan, wanted, nil)
	if err != nil {
		return nil, nil, err
	}
	replaced, prior, err := replacedProofs(ctx, store, held, operation, plan, block, carry && len(preparations) == 0)
	if err != nil {
		return nil, nil, err
	}
	return append(proofs, replaced...), append(prior, preparations...), nil
}

func removalClosure(plan reconciliation.Plan, dependencies []string) (map[string]bool, error) {
	wanted := map[string]bool{}
	pending := slices.Clone(dependencies)
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		if wanted[id] {
			continue
		}
		dependency, found := plan.Block(id)
		if !found {
			return nil, engineRecordsDamaged.Fail("the removal names a missing dependency", "")
		}
		wanted[id] = true
		pending = append(pending, dependency.Dependencies...)
	}
	return wanted, nil
}

// replacedProofs lends a replacement, nearest first, the dependency removals
// of its block that the same-source removals it replaces proved and that no
// nearer removal carries.
func replacedProofs(ctx context.Context, store recordReader, held lineage, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block, carry bool) ([]RemovalEvidence, []json.RawMessage, error) {
	carried := map[string]bool{}
	for _, candidate := range plan.Blocks {
		carried[candidate.ID] = true
	}
	var proofs []RemovalEvidence
	var preparations []json.RawMessage
	seen := map[string]bool{operation.ID: true}
	for id := operation.Replaces; id != ""; {
		if seen[id] || len(seen) > operationstore.MaxOperations {
			return nil, nil, engineRecordsDamaged.Fail("the removals this operation replaces name one another in a cycle or exceed the operations a context retains", "restore the context store")
		}
		seen[id] = true
		record, err := store.ReadOperation(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if record.Verb != reconciliation.Destroy || record.Source != operation.Source {
			return nil, nil, engineRecordsDamaged.Fail("the operation this removal replaces is no removal of the same apply", "restore the context store")
		}
		frozen, err := store.ReadPlan(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if own, found := frozen.Block(block.ID); found {
			if carry && len(preparations) == 0 && own.RequestDigest == block.RequestDigest {
				preparations, err = attemptPreparations(ctx, store, id, own.ID)
				if err != nil {
					return nil, nil, err
				}
			}
			wanted, err := removalClosure(frozen, own.Dependencies)
			if err != nil {
				return nil, nil, err
			}
			lent, err := provedRemovals(ctx, store, held, id, frozen, wanted, carried)
			if err != nil {
				return nil, nil, err
			}
			proofs = append(proofs, lent...)
		}
		for _, candidate := range frozen.Blocks {
			carried[candidate.ID] = true
		}
		id = record.Replaces
	}
	return proofs, preparations, nil
}

func provedRemovals(ctx context.Context, store recordReader, held lineage, operation string, plan reconciliation.Plan, wanted, carried map[string]bool) ([]RemovalEvidence, error) {
	var proofs []RemovalEvidence
	owned := held.owners()
	for _, dependency := range plan.Blocks {
		if !wanted[dependency.ID] || carried[dependency.ID] {
			continue
		}
		removed, err := blockEvidence(ctx, store, operation, dependency, reconciliation.Destroy)
		if err != nil {
			return nil, err
		}
		if removed.State != reconciliation.BlockDone || len(removed.Evidence) == 0 {
			continue
		}
		owner, err := owned.routed(dependency.ID)
		if err != nil {
			return nil, err
		}
		_, appliedBlock, found := owned.of(dependency.ID)
		if !found {
			return nil, engineRecordsDamaged.Fail("the removal dependency has no original apply identity", "")
		}
		applied, err := blockEvidence(ctx, store, owner.operation.ID, appliedBlock, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		removed.Block, _ = plan.Block(dependency.ID)
		applied.Block = appliedBlock
		removed.Evidence, applied.Evidence = slices.Clone(removed.Evidence), slices.Clone(applied.Evidence)
		proofs = append(proofs, RemovalEvidence{Removed: removed, Applied: applied})
	}
	return proofs, nil
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
	held, err := appliedLineage(ctx, store, operation)
	if err != nil {
		return nil, err
	}
	original, err := held.plan()
	if err != nil {
		return nil, err
	}
	if _, found := original.Block(block); !found {
		return nil, engineRecordsDamaged.Fail("the removal block has no original apply identity", "")
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
	owned := held.owners()
	for _, candidate := range original.Blocks {
		if candidate.ID == block || !wanted[candidate.ID] {
			continue
		}
		owner, err := owned.routed(candidate.ID)
		if err != nil {
			return nil, err
		}
		_, frozen, _ := owned.of(candidate.ID)
		proof, err := blockEvidence(ctx, store, owner.operation.ID, frozen, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		proof.Block = frozen
		proof.Evidence = slices.Clone(proof.Evidence)
		proved = append(proved, proof)
	}
	return proved, nil
}

func (s Service) populateRemovalExecution(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, execution *Execution) error {
	capability, _ := s.capabilities.Resolve(execution.Block.Kind, execution.Block.Implementation)
	carrier, carries := capability.(RemovalPreparationCarrier)
	proved, preparations, err := removalRecords(ctx, store, operation, plan, execution.Block, carries && carrier.CarryRemovalPreparation())
	if err != nil {
		return err
	}
	execution.RemovalProved, execution.RemovalPreparations = proved, preparations
	if operation.Verb != reconciliation.Destroy {
		return nil
	}
	execution.OriginalRemovalDependents, err = originalRemovalDependents(ctx, store, operation, execution.Block.ID)
	if err != nil {
		return err
	}
	if execution.Resolution == 0 && execution.Attempt > 0 {
		execution.PrepareRemoval = func(inner context.Context, preparation json.RawMessage) error {
			return store.RecordPreparation(inner, operation.ID, execution.Block.ID, execution.Attempt, slices.Clone(preparation))
		}
	}
	return nil
}
