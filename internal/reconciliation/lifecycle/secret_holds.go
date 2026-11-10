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
		chain, err := appliedLineage(ctx, store, current)
		if err != nil {
			return err
		}
		if current.Verb != reconciliation.Apply {
			plan, err := store.ReadPlan(ctx, current.ID)
			if err != nil {
				return err
			}
			chain = append(chain, lineageMember{operation: current, plan: plan})
		}
		for _, member := range chain {
			effect, classified := s.effectMaterial(member.plan)
			for _, binding := range member.operation.Bindings {
				holds.Bindings = append(holds.Bindings, custody.Hold{Binding: binding, Operation: holdingOperation(member.operation), Secrets: effect, All: !classified})
			}
		}
		return nil
	})
	if err != nil {
		return custody.Holds{}, err
	}
	return holds, nil
}

func holdingOperation(operation operationstore.Operation) string {
	return "the " + string(operation.Verb) + " " + operation.ID + " (" + string(operation.State) + ")"
}

func (s Service) effectMaterial(plan reconciliation.Plan) ([]string, bool) {
	var effect []string
	for _, block := range plan.Blocks {
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		held, classified := EffectMaterial(capability, block)
		if !classified {
			return nil, false
		}
		for _, reference := range held {
			if !slices.Contains(effect, reference) {
				effect = append(effect, reference)
			}
		}
	}
	slices.Sort(effect)
	return effect, true
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
