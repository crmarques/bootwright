package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type executionProof struct {
	proved  bool
	refusal error
}

func (s Service) otherBuild(operation operationstore.Operation) bool {
	current := operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit}
	return operation.AutomationDigest != s.automation.CatalogDigest() || operation.Executable != current || s.options.Executable.Modified
}

func (s Service) proveExecution(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) executionProof {
	if err := s.proveContracts(frozen, operation.Contracts); err != nil {
		return executionProof{refusal: engineOtherBuild.Fail(contractMessage(err), remediationFor(view, operation, s.canTakeBack(ctx, operation, frozen, states)))}
	}
	// Removal strictly reads every frozen request through its owning capability.
	// For a continued destroy it also proves the saved authorization and inverse.
	fresh := frozen
	var err error
	if operation.Verb == reconciliation.Destroy {
		fresh, err = s.removalOf(ctx, frozen, operation.Executable, frozenInputs(frozen))
	} else {
		for _, block := range frozen.Blocks {
			capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
			if !ok {
				err = engineOtherBuild.Fail("this executable provides no "+block.Implementation+" to read block "+block.ID, removeWith(operation.Executable))
				break
			}
			if _, why := capability.Removal(ctx, block); why != nil {
				err = unreadable(block, operation.Executable, why)
				break
			}
		}
	}
	if ctx.Err() != nil {
		return executionProof{refusal: ctx.Err()}
	}
	if err != nil {
		if s.canTakeBack(ctx, operation, frozen, states) {
			err = engineOtherBuild.Fail(firstMessage(err), remediationFor(view, operation, true))
		}
		return executionProof{refusal: err}
	}
	if operation.Verb == reconciliation.Destroy {
		if change, differs := reconciliation.Compare(frozen, fresh).First(); differs {
			return executionProof{refusal: engineOtherBuild.Fail("this executable would remove block "+change.Block+" differently from its frozen removal", remediationFor(view, operation, false))}
		}
	}
	return executionProof{proved: true}
}

func (s Service) canTakeBack(ctx context.Context, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) bool {
	if !supersedable(operation) {
		return false
	}
	owned, startedNothing := reconciliation.RemainingSubset(frozen, states), false
	if operation.Verb == reconciliation.Apply {
		owned, startedNothing = reconciliation.OwnedSubset(frozen, states), mayHaveStartedNothing(operation)
	}
	if len(owned.Blocks) == 0 && !startedNothing {
		return false
	}
	if s.proveContracts(owned, operation.Contracts) != nil {
		return false
	}
	_, err := s.removalOf(ctx, owned, operation.Executable, removalInputs{})
	return err == nil
}

func otherBuildRemediation(view View, operation operationstore.Operation) string {
	return remediationFor(view, operation, supersedable(operation))
}

func remediationFor(view View, operation operationstore.Operation, takeBack bool) string {
	registered := registeredWith(operation.Executable)
	if takeBack {
		return takeBackUnder(view.Identity().Name, "this executable") + ", or install " + registered + ", which registered it"
	}
	return "install " + registered + ", which registered this operation, and run bootwright install"
}
