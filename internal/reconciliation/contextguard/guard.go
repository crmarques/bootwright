// Package contextguard owns the local lifecycle evidence used during a locked
// Workspace mutation. It grants no authority to execute a native operation.
package contextguard

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type Guard struct{}

func (Guard) Check(ctx context.Context, data []byte) (contexts.Disposition, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	evidence, ok := reconciliation.ReadEvidence(data)
	if !ok {
		return contexts.Disposition{}, contexts.StateError("context mutation evidence is missing, corrupt or unsupported")
	}
	if err := ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	operation := evidence.Operation
	return contexts.Disposition{
		Update:  operation == reconciliation.MutationNone || operation == reconciliation.MutationApplied,
		Dispose: evidence == reconciliation.PristineEvidence(),
	}, nil
}
