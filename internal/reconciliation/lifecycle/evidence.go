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
		owners, err := evidenceOwners(ctx, store, lineageMember{operation: operation, plan: plan})
		if err != nil {
			return err
		}
		for _, owner := range owners {
			for _, block := range owner.plan.Blocks {
				if block.Kind != kind || block.Object != object {
					continue
				}
				evidence, err := blockEvidence(ctx, store, owner.operation.ID, block, owner.operation.Verb)
				if err != nil {
					return err
				}
				found = append(found, evidence)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func evidenceOwners(ctx context.Context, store recordReader, current lineageMember) ([]lineageMember, error) {
	if current.operation.Verb != reconciliation.Apply || current.operation.Predecessor == "" {
		return []lineageMember{current}, nil
	}
	return lineageOf(ctx, store, current)
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
	if len(block.Prerequisites) == 0 {
		return proved, nil
	}
	held, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: plan})
	if err != nil {
		return nil, err
	}
	for _, id := range block.Prerequisites {
		owner, found := held.ownerOf(id)
		if !found {
			return nil, failure("lifecycle.state", "no apply of the lineage of "+operation.ID+" holds the prerequisite "+id, "restore the context store")
		}
		prerequisite, _ := owner.plan.Block(id)
		evidence, err := blockEvidence(ctx, store, owner.operation.ID, prerequisite, operation.Verb)
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
	held := lineage{{operation: operation, plan: plan}}
	if operation.Predecessor != "" {
		if held, err = lineageOf(ctx, store, held[0]); err != nil {
			return nil, err
		}
		if plan, err = held.plan(); err != nil {
			return nil, err
		}
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
		owner, _ := held.ownerOf(dependency.ID)
		evidence, err := blockEvidence(ctx, store, owner.operation.ID, dependency, reconciliation.Apply)
		if err != nil {
			return nil, err
		}
		evidence.Block, _ = owner.plan.Block(dependency.ID)
		evidence.Evidence = slices.Clone(evidence.Evidence)
		proved = append(proved, evidence)
	}
	return proved, nil
}

func applyRecords(ctx context.Context, store recordReader, applyOperation, blockID string) ([]BlockEvidence, []json.RawMessage, error) {
	proved, err := applyDependencies(ctx, store, applyOperation, blockID)
	if err != nil {
		return nil, nil, err
	}
	preparations, err := applyPreparations(ctx, store, applyOperation, blockID)
	return proved, preparations, err
}

func applyPreparations(ctx context.Context, store recordReader, applyOperation, blockID string) ([]json.RawMessage, error) {
	operation, err := store.ReadOperation(ctx, applyOperation)
	if err != nil {
		return nil, err
	}
	if operation.Verb != reconciliation.Apply {
		return nil, failure("lifecycle.state", "dependency identity must come from the original apply", "")
	}
	record, err := store.Block(ctx, applyOperation, blockID)
	if err != nil {
		return nil, err
	}
	var preparations []json.RawMessage
	for number := 1; number <= record.Attempts; number++ {
		attempt, err := store.Attempt(ctx, applyOperation, blockID, number)
		if err != nil {
			return nil, err
		}
		if len(attempt.Preparation) != 0 {
			preparations = append(preparations, slices.Clone(attempt.Preparation))
		}
	}
	return preparations, nil
}

func clonedPreparations(preparations []json.RawMessage) []json.RawMessage {
	var cloned []json.RawMessage
	for _, preparation := range preparations {
		cloned = append(cloned, slices.Clone(preparation))
	}
	return cloned
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
