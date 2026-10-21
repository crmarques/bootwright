package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type ProducedInput struct {
	Kind   string `json:"kind"`
	Object string `json:"object"`
	Output string `json:"output"`
}

type producedRead func(context.Context, string, string) (secretstore.ProducedMaterial, bool, error)

func (s Service) producedReferences(block reconciliation.Block) ([]ProducedInput, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return nil, failure("lifecycle.state", "the produced input consumer does not resolve", "")
	}
	consumer, ok := capability.(ProducedInputConsumer)
	if !ok {
		return nil, nil
	}
	refs, err := consumer.ProducedInputs(block)
	if err != nil {
		return nil, err
	}
	for i, ref := range refs {
		if ref.Kind == "" || ref.Object == "" || !reconciliation.ValidSegment(ref.Output) || slices.Contains(refs[:i], ref) {
			return nil, failure("lifecycle.state", "block "+block.ID+" names an invalid or repeated produced input", "repair the package's produced input references")
		}
	}
	return slices.Clone(refs), nil
}

func (s Service) producedProducer(ref ProducedInput, definitions []reconciliation.BlockDefinition) (string, error) {
	producer := ""
	for _, definition := range definitions {
		if definition.Kind != ref.Kind || definition.Object != ref.Object {
			continue
		}
		capability, ok := s.capabilities.Resolve(definition.Kind, definition.Implementation)
		if !ok {
			continue
		}
		provider, ok := capability.(ProducedOutputProvider)
		if !ok {
			continue
		}
		outputs, err := provider.ProducedOutputs(reconciliation.Block{BlockDefinition: definition})
		if err != nil {
			return "", err
		}
		if !slices.Contains(outputs, ref.Output) {
			continue
		}
		if producer != "" {
			return "", failure("lifecycle.state", "produced input "+ref.Kind+"/"+ref.Object+"/"+ref.Output+" has several producers", "select one producer for this output")
		}
		producer = definition.ID
	}
	if producer == "" {
		return "", failure("lifecycle.state", "produced input "+ref.Kind+"/"+ref.Object+"/"+ref.Output+" has no producer", "include the producer in this plan")
	}
	return producer, nil
}

func (s Service) dependOnProducedInputs(definitions []reconciliation.BlockDefinition) ([]reconciliation.BlockDefinition, error) {
	result := slices.Clone(definitions)
	for i, definition := range result {
		refs, err := s.producedReferences(reconciliation.Block{BlockDefinition: definition})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			producer, err := s.producedProducer(ref, definitions)
			if err != nil {
				return nil, err
			}
			result[i].Dependencies = append(slices.Clone(result[i].Dependencies), producer)
		}
		slices.Sort(result[i].Dependencies)
		result[i].Dependencies = slices.Compact(result[i].Dependencies)
	}
	return result, nil
}

func (s Service) producedLender(block reconciliation.Block, definitions []reconciliation.BlockDefinition, removal bool, read producedRead) (func(context.Context, ProducedInput) (secrets.Material, error), func(), error) {
	refs, err := s.producedReferences(block)
	if err != nil {
		return nil, nil, err
	}
	producers := map[ProducedInput]string{}
	for _, ref := range refs {
		producer, err := s.producedProducer(ref, definitions)
		if err != nil {
			return nil, nil, err
		}
		producers[ref] = producer
	}
	var mutex sync.Mutex
	var lent []secrets.Material
	closed := false
	release := func() {
		mutex.Lock()
		defer mutex.Unlock()
		closed = true
		for _, material := range lent {
			material.Clear()
		}
		lent = nil
	}
	lend := func(ctx context.Context, ref ProducedInput) (secrets.Material, error) {
		mutex.Lock()
		defer mutex.Unlock()
		if err := ctx.Err(); err != nil {
			return secrets.Material{}, err
		}
		producer, declared := producers[ref]
		if closed || !declared {
			return secrets.Material{}, failure("lifecycle.state", "block "+block.ID+" cannot borrow an undeclared or expired produced input", "repeat the operation with its frozen input")
		}
		entry, found, err := read(ctx, producer, ref.Output)
		if err != nil {
			entry.Material.Clear()
			return secrets.Material{}, err
		}
		if !found || entry.Unproved && !removal {
			entry.Material.Clear()
			return secrets.Material{}, failure("lifecycle.state", "produced input "+producer+"/"+ref.Output+" is missing or unproved", "complete the producer block "+producer+" before applying this consumer")
		}
		lent = append(lent, entry.Material)
		return entry.Material, nil
	}
	return lend, release, nil
}

func planDefinitions(plan reconciliation.Plan) []reconciliation.BlockDefinition {
	result := make([]reconciliation.BlockDefinition, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		result = append(result, block.BlockDefinition)
	}
	return result
}

func (s Service) producedInTransaction(tx Transaction) producedRead {
	return func(ctx context.Context, block, output string) (secretstore.ProducedMaterial, bool, error) {
		binder, ok := s.binder.(ProducedBinder)
		if !ok {
			return secretstore.ProducedMaterial{}, false, failure("lifecycle.state", "produced input lending is unavailable", "")
		}
		var material secretstore.ProducedMaterial
		var found bool
		err := tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			var err error
			material, found, err = binder.ReadProducedInArea(ctx, selected, area, block, output)
			return err
		})
		return material, found, err
	}
}

func (s Service) producedInContext(name string) producedRead {
	return func(ctx context.Context, block, output string) (secretstore.ProducedMaterial, bool, error) {
		binder, ok := s.binder.(ProducedBinder)
		if !ok {
			return secretstore.ProducedMaterial{}, false, failure("lifecycle.state", "produced input lending is unavailable", "")
		}
		return binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: name, Block: block, Name: output})
	}
}

func (s Service) prepareProducedExecution(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, execution *Execution) (func(), error) {
	registered := operation.Created
	if operation.Verb == reconciliation.Destroy {
		var err error
		plan, err = store.ReadPlan(ctx, operation.Source)
		if err != nil {
			return nil, err
		}
		original, err := store.ReadOperation(ctx, operation.Source)
		if err != nil {
			return nil, err
		}
		registered = original.Created
	}
	lend, release, err := s.producedLender(execution.Block, planDefinitions(plan), operation.Verb == reconciliation.Destroy, s.producedInTransaction(tx))
	if err != nil {
		return nil, err
	}
	execution.LendProduced = lend
	execution.ApplyRegistered = registered
	previous, err := previousBlockEvidence(ctx, store, owningApply(operation), execution.Block.ID)
	if err != nil {
		release()
		return nil, err
	}
	execution.PreviousEvidence = previous
	return release, nil
}

func previousBlockEvidence(ctx context.Context, store recordReader, operation, block string) (json.RawMessage, error) {
	record, err := store.Block(ctx, operation, block)
	if err != nil {
		return nil, err
	}
	for number := record.Attempts; number > 0; number-- {
		observed, found, err := store.LastResolution(ctx, operation, block, number)
		if err != nil {
			return nil, err
		}
		if found && observed.Phase == "observed" && len(observed.Evidence) > 0 && string(observed.Evidence) != "null" {
			return slices.Clone(observed.Evidence), nil
		}
		attempted, err := store.Attempt(ctx, operation, block, number)
		if err != nil {
			return nil, err
		}
		if attempted.Phase == "observed" && len(attempted.Evidence) > 0 && string(attempted.Evidence) != "null" {
			return slices.Clone(attempted.Evidence), nil
		}
	}
	return nil, nil
}
