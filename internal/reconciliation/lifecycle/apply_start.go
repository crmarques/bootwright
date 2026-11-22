package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type applyStartChecker interface {
	CheckApplyStart(context.Context, Probe) error
}

func (s Service) checkApplyStart(ctx context.Context, run invocation, states map[string]reconciliation.BlockState) error {
	for _, block := range run.plan.Blocks {
		if !run.effects[block.ID] && !unproved(states[block.ID]) {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		checker, ok := capability.(applyStartChecker)
		if !ok {
			continue
		}
		owner, err := ownerID(ctx, run.store, run.operation, block.ID)
		if err != nil {
			return err
		}
		if err := checker.CheckApplyStart(ctx, Probe{Context: run.tx.Identity().Name, Block: block, ApplyOperation: owner, Material: run.material}); err != nil {
			return err
		}
	}
	return nil
}
