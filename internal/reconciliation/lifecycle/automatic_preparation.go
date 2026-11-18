package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (d PreparationDefinition) admitsCommand(command string) bool {
	return command == d.Command || command == string(reconciliation.Apply) && d.Automatic != nil
}

func (s Service) automaticPreparation(ctx context.Context, name string, selection reconciliation.Selection, preview bool) (*reconciliation.PreparationScope, bool, error) {
	if len(s.options.Preparations) == 0 {
		return nil, false, nil
	}
	var scope *reconciliation.PreparationScope
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		if index.Current != "" {
			operation, err := store.ReadOperation(ctx, index.Current)
			if err != nil {
				return err
			}
			if operation.State != reconciliation.OperationDone {
				frozen, err := store.ReadPlan(ctx, operation.ID)
				if err != nil {
					return err
				}
				if operation.Verb == reconciliation.Apply && frozen.Preparation != nil && (preview || frozen.Preparation.Command == string(reconciliation.Apply)) {
					scope = frozen.Preparation
				}
				return nil
			}
		}
		state, err := s.compile(ctx, view)
		if err != nil {
			return err
		}
		controller, err := controllerMachine(state)
		if err != nil {
			return err
		}
		input := s.planInput(view, state, controller, nil)
		for _, definition := range s.options.Preparations {
			if definition.Automatic == nil {
				continue
			}
			targets, err := definition.Automatic(ctx, input, selection)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				continue
			}
			if scope != nil {
				return engineRequestDefect.Fail("multiple preparations compete for the next apply phase", "run each owner's explicit preparation command")
			}
			targets = slices.Clone(targets)
			slices.Sort(targets)
			scope = &reconciliation.PreparationScope{Kind: definition.Kind, Command: string(reconciliation.Apply), Targets: slices.Compact(targets), PreparePrerequisites: true}
		}
		return nil
	})
	if err != nil || scope == nil {
		return nil, false, err
	}
	if selection.Until != "" {
		return nil, false, applyUntilBeforePreparation.Fail("the add-on boundary cannot be planned before preparation completes", "run "+contextCommand(name, "apply")+" to complete preparation and review the resolved installation plan before selecting --until")
	}
	prepared, err := s.admitPreparation(*scope)
	return prepared, err == nil, err
}

func (s Service) applyPrepared(ctx context.Context, request ApplyRequest, selection reconciliation.Selection, scope *reconciliation.PreparationScope) (*OperationResult, error) {
	prepared, automatic := scope, false
	if prepared == nil {
		var err error
		if prepared, automatic, err = s.automaticPreparation(ctx, request.ContextName, selection, false); err != nil {
			return nil, err
		}
	}
	result, err := s.mutate(ctx, reconciliation.Apply, request.ContextName, selection, request.Authorizations, nil, request.SkipConfirmation, request.Unattended, request.SSH.Borrowed(), prepared)
	if err != nil || !automatic || result == nil || result.Receipt.State != string(reconciliation.OperationDone) || !selection.Stages.Selects(reconciliation.StageMachines) {
		return result, err
	}
	next, err := s.mutate(ctx, reconciliation.Apply, request.ContextName, selection, request.Authorizations, nil, request.SkipConfirmation, request.Unattended, request.SSH.Borrowed(), nil)
	if next == nil && err != nil {
		return result, err
	}
	return next, err
}
