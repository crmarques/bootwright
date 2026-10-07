// Package contextguard owns the local lifecycle evidence used during a locked
// Workspace mutation. It grants no authority to execute a native operation.
package contextguard

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type Guard struct{}

// Check grants a disposition from the one reading of the evidence. Evidence it
// cannot read grants none and refuses with contexts.ErrUnreadableEvidence,
// over which only the orphan acknowledgement deletes the context.
func (Guard) Check(ctx context.Context, data []byte) (contexts.Disposition, error) {
	if err := ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	evidence, ok := reconciliation.ReadEvidence(data)
	if !ok {
		return contexts.Disposition{}, &unreadableEvidence{contexts.StateError("context mutation evidence is missing, corrupt or unsupported")}
	}
	if err := ctx.Err(); err != nil {
		return contexts.Disposition{}, err
	}
	operation := evidence.Operation
	return contexts.Disposition{
		Update:  operation == reconciliation.MutationNone || operation == reconciliation.MutationApplied,
		Dispose: evidence == reconciliation.PristineEvidence(),
		Applied: operation == reconciliation.MutationApplied,
	}, nil
}

// unreadableEvidence is the refusal of evidence the guard cannot read, which
// the context service tells from every other refusal by
// contexts.ErrUnreadableEvidence; it reports the context.state diagnostic.
type unreadableEvidence struct{ failure error }

func (e *unreadableEvidence) Error() string { return e.failure.Error() }

func (e *unreadableEvidence) Unwrap() error { return e.failure }

func (e *unreadableEvidence) Is(target error) bool { return target == contexts.ErrUnreadableEvidence }
