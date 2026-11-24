package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type destroyStartPrompter interface {
	PromptDestroyStart(context.Context, Probe) error
}

func (s Service) promptStart(ctx context.Context, name string, decided operationPlan) error {
	switch decided.verb {
	case reconciliation.Apply:
		return s.promptApplyStart(ctx, name, decided)
	case reconciliation.Destroy:
		return s.promptDestroyStart(ctx, name, decided)
	}
	return nil
}

// promptDestroyStart asks before the operation starts, so no prompt waits while
// cleanup settles. A step re-asks a password unread here and refuses on its own,
// so a keep-going removal still completes what does not depend on that Machine.
func (s Service) promptDestroyStart(ctx context.Context, name string, decided operationPlan) error {
	for _, block := range decided.plan.Blocks {
		if decided.states[block.ID] == reconciliation.BlockDone || block.Relaxation == Abandon {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		if prompter, ok := capability.(destroyStartPrompter); ok {
			prompter.PromptDestroyStart(ctx, Probe{Context: name, Block: block})
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}
