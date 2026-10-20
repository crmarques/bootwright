package lifecycle

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// HandoffSummary reports whether durable records prove the apply ready for handoff.
type HandoffSummary struct {
	Ready   bool
	Reasons []string
}

func handoff(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) HandoffSummary {
	result := HandoffSummary{Reasons: []string{}}
	if operation.ID == "" {
		result.Reasons = append(result.Reasons, "no apply is recorded")
		return result
	}
	if operation.Verb != reconciliation.Apply {
		result.Reasons = append(result.Reasons, "the latest operation is a destroy")
	} else if operation.State != reconciliation.OperationDone {
		result.Reasons = append(result.Reasons, fmt.Sprintf("the apply is %s, not done", operation.State))
	}
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		if state != reconciliation.BlockDone {
			result.Reasons = append(result.Reasons, fmt.Sprintf("block %s is %s, not done", block.ID, state))
		}
	}
	for _, block := range frozen.Blocks {
		if states[block.ID] == reconciliation.BlockUnknown || states[block.ID] == reconciliation.BlockRunning {
			result.Reasons = append(result.Reasons, "block "+block.ID+" has an unresolved unknown outcome")
		}
	}
	if operation.LogFault {
		result.Reasons = append(result.Reasons, "the operation has a latched required-log fault")
	}
	result.Ready = len(result.Reasons) == 0
	return result
}
