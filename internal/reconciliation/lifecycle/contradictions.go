package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// refuseContradictions refuses the removal of an incomplete apply whose records
// prove it started a block they no longer show as started. Such a block reads
// back as pending, so a removal planned from these records would skip it, leave
// its effect in place and then release the binding that effect needs. It reads
// only, under the lock the decision holds, before anything is presented,
// bound, probed, registered or released.
func refuseContradictions(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	lost, err := store.LostBlockRecords(ctx, operation.ID, frozen)
	if err != nil {
		return err
	}
	entries := contradictions(operation, frozen, states, lost)
	if len(entries) == 0 {
		return nil
	}
	return failure("lifecycle.state",
		"the incomplete apply "+operation.ID+" holds records that contradict what it started, and a removal that skipped such a block would leave its effect in place: "+strings.Join(entries, ", "),
		"review its durable state with bootwright status")
}

// contradictions names what an apply's own records prove impossible, per block
// in frozen order and then for the operation: a pending block a direct
// dependent of which started, since a block starts only once every dependency
// is done; a block that lost its record beside an attempt of it, since a start
// publishes the block record first; and a failed apply with no failed, running
// or unknown block, since an apply records failed only once a block failed, a
// failed block changes only through a retry that first marks the apply
// running, and a running or unknown block changes only through a resolution,
// before which a continuation marks the apply running and a removal records a
// failed apply in the state its blocks give it, which is failed only while
// another block is. A retry whose start published its running record and then
// reported a failure leaves the apply failed beside that running block, which
// is no contradiction: a running or unknown block started and its outcome is
// unproved, so the removal resolves it before it registers. An unknown apply
// without an unknown block is not one of them either: a removal interrupted
// after resolving that block and before recording the apply leaves it so.
func contradictions(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, lost []string) []string {
	read := func(id string) reconciliation.BlockState {
		if state := states[id]; state != "" {
			return state
		}
		return reconciliation.BlockPending
	}
	entries := []string{}
	accounted := false
	for _, block := range frozen.Blocks {
		switch read(block.ID) {
		case reconciliation.BlockFailed, reconciliation.BlockRunning, reconciliation.BlockUnknown:
			accounted = true
		}
		if read(block.ID) == reconciliation.BlockPending {
			for _, dependent := range frozen.Blocks {
				if slices.Contains(dependent.Dependencies, block.ID) && read(dependent.ID) != reconciliation.BlockPending {
					entries = append(entries, block.ID+" (pending, yet "+dependent.ID+", which depends on it, started)")
					break
				}
			}
		}
		if slices.Contains(lost, block.ID) {
			entries = append(entries, block.ID+" (no block record, yet an attempt of it is recorded)")
		}
	}
	if operation.State == reconciliation.OperationFailed && !accounted {
		entries = append(entries, "the apply records failed, yet no block records the failure")
	}
	return entries
}
