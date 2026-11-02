package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) finishSettled(ctx context.Context, name string, decided operationPlan, identity ContextIdentity, finalized bool, stranded []string) (*OperationResult, error) {
	// A settle registers nothing and performs no host effect, so it needs no
	// authorization or confirmation. It restores a latched apply log fault
	// before reporting success and names a finalization it completed first.
	//
	// A destroy settles only over pristine evidence and no reservation, where
	// no operation names a binding, so every binding it listed before it read
	// that evidence is stranded and released, still without a transaction. An
	// apply in flight that bound one of them raised the evidence before it
	// bound, so a release lowered it since and that apply refuses at its
	// re-proof rather than register the binding. A removal's finalization this
	// invocation completed already collected what it listed after this one.
	// A claim that holds nothing beside it is what an apply stopped before its
	// running evidence landed leaves, and it is reclaimed in a transaction that
	// does nothing else.
	if decided.verb == reconciliation.Apply && decided.operation.LogFault {
		if err := s.restoreSettledApply(ctx, name, decided); err != nil {
			return nil, err
		}
	}
	if decided.idle {
		s.reclaimIdle(ctx, name)
	}
	if decided.verb == reconciliation.Destroy && !finalized {
		s.collect(ctx, name, stranded, nil)
	}
	recovered := ""
	if finalized {
		recovered = RecoveredFinalization
	}
	return settled(identity, decided, recovered), nil
}

// settled reports the verb whose work durable state already proves. It touches
// no record, so the operation it names keeps the state and identity it
// finished with. recovered names what the invocation completed before it
// settled, if anything.
func settled(identity ContextIdentity, decided operationPlan, recovered string) *OperationResult {
	operation := decided.operation.ID
	if operation == "" {
		operation = "none"
	}
	return &OperationResult{
		Context:   identity,
		Verb:      string(decided.verb),
		Steps:     steps(decided.plan, decided.states),
		Blocks:    blockResults(decided.plan, decided.states),
		Settled:   true,
		Recovered: recovered,
		Receipt: Receipt{
			Operation: operation, Verb: string(decided.verb),
			State: string(reconciliation.OperationDone), Next: "none",
		},
	}
}
