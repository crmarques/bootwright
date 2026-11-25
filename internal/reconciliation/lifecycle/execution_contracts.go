package lifecycle

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) executionContracts(plan reconciliation.Plan) (map[string]string, error) {
	contracts := map[string]string{}
	for _, block := range plan.Blocks {
		if s.options.Contracts == nil {
			return nil, contractFailure("execution contract for block %s is unavailable", block.ID)
		}
		current, ok := s.options.Contracts(block)
		decoded, err := hex.DecodeString(current.Fingerprint)
		if !ok || current.ContentDigest != block.ContentDigest || err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != current.Fingerprint {
			return nil, contractFailure("this executable does not support the execution contract of block %s", block.ID)
		}
		if held, found := contracts[block.Implementation]; found && held != current.Fingerprint {
			return nil, contractFailure("block %s names a conflicting execution contract", block.ID)
		}
		contracts[block.Implementation] = current.Fingerprint
	}
	return contracts, nil
}

func (s Service) freezeExecutionPlan(plan reconciliation.Plan, scope *reconciliation.PreparationScope) (reconciliation.Plan, error) {
	plan, err := plan.WithPreparation(scope)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	_, err = s.executionContracts(plan)
	return plan, err
}

func (s Service) proveContracts(plan reconciliation.Plan, held map[string]string) error {
	current, err := s.executionContracts(plan)
	if err != nil {
		return err
	}
	for _, block := range plan.Blocks {
		if current[block.Implementation] != held[block.Implementation] {
			return contractFailure("this executable does not support the recorded execution contract of block %s", block.ID)
		}
	}
	return nil
}

func contractFailure(format string, args ...any) error {
	return engineOtherBuild.Fail(fmt.Sprintf(format, args...), "use the executable that registered the operation")
}

func contractMessage(err error) string {
	if reported := diagnostics.Of(err); len(reported) != 0 {
		return reported[0].Message
	}
	return err.Error()
}

func (s Service) proveLineageContracts(chain lineage) error {
	for index, created := range chain.owners().incarnated() {
		if err := s.proveContracts(created, chain[index].operation.Contracts); err != nil {
			return engineOtherBuild.Fail(contractMessage(err), removeWith(chain[index].operation.Executable))
		}
	}
	return nil
}

func (s Service) proveOwnedContract(ctx context.Context, store recordReader, owner string, block reconciliation.Block) error {
	operation, err := store.ReadOperation(ctx, owner)
	if err != nil {
		return err
	}
	return s.proveContracts(reconciliation.Plan{Blocks: []reconciliation.Block{block}}, operation.Contracts)
}
