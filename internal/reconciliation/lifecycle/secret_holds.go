package lifecycle

import (
	"bytes"
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

func (s Service) SecretHolds(ctx context.Context, name string) (custody.Holds, error) {
	if err := s.available(ctx); err != nil {
		return custody.Holds{}, err
	}
	running, err := projection(reconciliation.Apply, reconciliation.OperationRunning)
	if err != nil {
		return custody.Holds{}, err
	}
	var holds custody.Holds
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		raised := bytes.Equal(view.Evidence(), running)
		if index.Current == "" {
			holds.Registering = raised
			return nil
		}
		current, err := store.ReadOperation(ctx, index.Current)
		if err != nil {
			return err
		}
		holds.Registering = raised && (current.Verb != reconciliation.Apply || current.State != reconciliation.OperationRunning)
		plan, err := store.ReadPlan(ctx, current.ID)
		if err != nil {
			return err
		}
		states, err := store.BlockStates(ctx, current.ID, plan)
		if err != nil {
			return err
		}
		chain, err := ownedLineage(ctx, store, lineageMember{operation: current, plan: plan, states: states})
		if err != nil {
			return err
		}
		holds.Bindings = s.stepHolds(chain)
		return nil
	})
	if err != nil {
		return custody.Holds{}, err
	}
	return holds, nil
}

// stepHolds holds each step's effect material until a removal proves it gone.
func (s Service) stepHolds(chain lineage) []custody.Hold {
	var holds []custody.Hold
	at := map[string]int{}
	for _, member := range chain {
		for _, binding := range member.operation.Bindings {
			if _, named := at[binding]; !named {
				at[binding] = len(holds)
				holds = append(holds, custody.Hold{Binding: binding, Operation: holdingOperation(member.operation)})
			}
		}
	}
	presents := chain.presenting()
	for _, holding := range chain.ownership().Holdings() {
		if chain[holding.Holder].operation.Verb == reconciliation.Destroy && holding.State == reconciliation.BlockDone && holding.Life == reconciliation.LifeAbsent {
			continue
		}
		capability, _ := s.capabilities.Resolve(holding.Block.Kind, holding.Block.Implementation)
		effect, classified := EffectMaterial(capability, holding.Block)
		bindings := joinBindings(nil, presents.of(holding.Block.ID))
		if holding.Incarnation >= 0 {
			bindings = joinBindings(bindings, chain[holding.Incarnation].operation.Bindings...)
		}
		for _, binding := range bindings {
			index, named := at[binding]
			if !named {
				continue
			}
			holds[index].All = holds[index].All || !classified
			holds[index].Secrets = joinBindings(holds[index].Secrets, effect...)
		}
	}
	for index := range holds {
		slices.Sort(holds[index].Secrets)
	}
	return holds
}

func holdingOperation(operation operationstore.Operation) string {
	return "the " + string(operation.Verb) + " " + operation.ID + " (" + string(operation.State) + ")"
}

func EffectMaterial(capability Capability, block reconciliation.Block) ([]string, bool) {
	referencer, named := capability.(SecretReferencer)
	classifier, classifies := capability.(SecretClassifier)
	if !named || !classifies {
		return nil, false
	}
	references, err := referencer.SecretReferences(block)
	if err != nil {
		return nil, false
	}
	classes, err := classifier.SecretClasses(block)
	if err != nil {
		return nil, false
	}
	var effect []string
	for _, reference := range references {
		if class := classes.Of(reference); class != secrets.AccessCredential && class != secrets.TrustAnchor {
			effect = append(effect, reference)
		}
	}
	return effect, true
}
