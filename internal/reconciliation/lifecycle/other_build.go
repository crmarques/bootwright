package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type sameBlocksProof struct {
	proved  bool
	refusal error
}

func (s Service) otherBuild(operation operationstore.Operation) bool {
	current := operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit}
	return operation.AutomationDigest != s.automation.CatalogDigest() || operation.Executable != current || s.options.Executable.Modified
}

func (s Service) proveSameBlocks(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) sameBlocksProof {
	if !s.otherBuild(operation) {
		return sameBlocksProof{proved: true}
	}
	fresh, err := s.replanned(ctx, view, operation, frozen)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return sameBlocksProof{refusal: ctxErr}
	}
	remediation := remediationFor(view, operation, s.canTakeBack(ctx, operation, frozen, states))
	if err != nil {
		detail := ""
		if reported := diagnostics.Of(err); len(reported) != 0 {
			detail = ": " + reported[0].Message
		}
		return sameBlocksProof{refusal: failure("lifecycle.state",
			"this executable cannot plan this operation again to prove it freezes the same blocks"+detail, remediation)}
	}
	if change, differs := reconciliation.Compare(frozen, fresh).First(); differs {
		return sameBlocksProof{refusal: failure("lifecycle.state",
			"this executable would freeze the block "+change.Block+" differently from the plan this operation froze", remediation)}
	}
	return sameBlocksProof{proved: true}
}

func (s Service) replanned(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan) (reconciliation.Plan, error) {
	if operation.Verb == reconciliation.Destroy {
		return s.removalOf(ctx, frozen, operation.Executable)
	}
	state, err := s.compile(ctx, view)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	if err := s.refuseUnsupported(state); err != nil {
		return reconciliation.Plan{}, err
	}
	plan, _, err := s.planFrom(ctx, view, state, reconciliation.Apply)
	return plan, err
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
	_, err := s.removalOf(ctx, owned, operation.Executable)
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
	return "install " + registered + ", which registered this operation, and run bootwright setup"
}
