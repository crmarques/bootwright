package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const failedWithoutFailure = "the apply records failed, yet no block records the failure"

const unreadableEvidenceExit = "restore the whole store from a matching backup, then retry: context deletion refuses mutation evidence it cannot read, with --allow-orphans or without"

// deletionExit is the remedy of the record states neither verb acts on: a lost
// index beside what no index accounts for, and a completed removal holding a
// block that is not done. Deleting the context is their only exit while the
// context guard reads its evidence, so the remedy names the deletion the guard
// admits, from the guard's own reading: evidence read as pristine admits a
// plain deletion, and any other it reads protects the context, so the
// deletion must acknowledge the objects it abandons. Evidence the guard cannot
// read admits no deletion at all, so over it the remedy names none.
func deletionExit(view View) string {
	evidence, readable := reconciliation.ReadEvidence(view.Evidence())
	if !readable {
		return unreadableEvidenceExit
	}
	command := "bootwright context delete --name " + view.Identity().Name + " --purge"
	if evidence == reconciliation.PristineEvidence() {
		return "delete the context with " + command
	}
	return "delete the context with " + command + " --allow-orphans, which abandons what it may still own"
}

// refuseContradictions refuses the removal of an incomplete apply whose records
// prove it started a block they no longer show as started. Such a block reads
// back as pending, so a removal planned from these records would skip it, leave
// its effect in place and then release the binding that effect needs. It reads
// only, under the lock the decision holds, before anything is presented,
// bound, probed, registered or released.
func refuseContradictions(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) error {
	lost, err := store.LostBlockRecords(ctx, operation.ID, frozen)
	if err != nil {
		return err
	}
	entries := contradictions(operation, frozen, states, attempts, lost)
	if len(entries) == 0 {
		return nil
	}
	return failure("lifecycle.state",
		"the incomplete apply "+operation.ID+" holds records that contradict what it started, and a removal that skipped such a block would leave its effect in place: "+strings.Join(entries, ", "),
		"review its durable state with bootwright status")
}

// contradictions names what an apply's own records prove impossible, per block
// in frozen order and then for the operation: a block that is not done while a
// direct dependent of it started, since a block starts only once every
// dependency is done and a done block never changes; a block that lost its
// record beside an attempt of it, since a start publishes the block record
// first; and a failed apply holding a block that is not done, yet no failed or
// unknown block and no running block its record counts as retried, since an
// apply records failed only once a block failed, a failed block changes only
// through a retry that first marks the apply running, and a running or unknown
// block changes only through a resolution, before which a continuation marks
// the apply running and a removal records a failed apply in the state its
// blocks give it, which is failed only while another block is. A failed apply
// whose blocks are all done is no contradiction: its record lags them, and a
// removal takes back its whole frozen plan. A retry whose start published its
// running record and then reported a failure leaves the apply failed beside
// that running block, which is no contradiction: the block counts the attempt
// it retried as well, and its outcome is unproved, so the removal resolves it
// before it registers. A running block with a single attempt is never that
// retry, so it accounts for nothing. An unknown apply without an unknown block
// is not one of them either: a removal interrupted after resolving that block
// and before recording the apply leaves it so.
func contradictions(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int, lost []string) []string {
	read := func(id string) reconciliation.BlockState {
		if state := states[id]; state != "" {
			return state
		}
		return reconciliation.BlockPending
	}
	entries := []string{}
	accounted := false
	for _, block := range frozen.Blocks {
		state := read(block.ID)
		switch {
		case state == reconciliation.BlockFailed, state == reconciliation.BlockUnknown:
			accounted = true
		case state == reconciliation.BlockRunning && attempts[block.ID] > 1:
			accounted = true
		}
		if state != reconciliation.BlockDone {
			for _, dependent := range frozen.Blocks {
				if slices.Contains(dependent.Dependencies, block.ID) && read(dependent.ID) != reconciliation.BlockPending {
					entries = append(entries, block.ID+" ("+string(state)+", yet "+dependent.ID+", which depends on it, started)")
					break
				}
			}
		}
		if slices.Contains(lost, block.ID) {
			entries = append(entries, lostRecord(block.ID))
		}
	}
	if operation.State == reconciliation.OperationFailed && !accounted && pendingRemains(frozen, states) {
		entries = append(entries, failedWithoutFailure)
	}
	return entries
}

func lostRecord(block string) string {
	return block + " (no block record, yet an attempt of it is recorded)"
}

// refuseUncontinuable refuses a continuation over records that contradict what
// its operation started, before it restores, raises or marks anything: a block
// that lost its record beside an attempt of it, whose start would refuse
// rather than skip the observation an effect that may have begun requires. A
// failed apply whose every block is done never reaches it, because its own
// verb finalizes it first.
func refuseUncontinuable(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan) error {
	entries, err := uncontinuable(ctx, store, operation, frozen)
	if err != nil || len(entries) == 0 {
		return err
	}
	return failure("lifecycle.state",
		"the incomplete "+string(operation.Verb)+" "+operation.ID+" holds records that contradict what it started, so it cannot be continued: "+strings.Join(entries, ", "),
		"review its durable state with bootwright status")
}

func uncontinuable(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan) ([]string, error) {
	lost, err := store.LostBlockRecords(ctx, operation.ID, frozen)
	if err != nil {
		return nil, err
	}
	entries := make([]string, 0, len(lost))
	for _, block := range lost {
		entries = append(entries, lostRecord(block))
	}
	return entries, nil
}

// recordContradictions names everything the current operation's records
// contradict, as the refusals that point at bootwright status name it: each
// block a completed operation's records do not show done, what an incomplete
// apply's records prove impossible and an incomplete apply that started no
// block although its own state says it did, and each block of an incomplete
// removal that lost its record beside an attempt of it.
func recordContradictions(ctx context.Context, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) ([]string, error) {
	if operation.State == reconciliation.OperationDone {
		return unfinishedBlocks(frozen, states), nil
	}
	if operation.Verb == reconciliation.Destroy {
		return uncontinuable(ctx, store, operation, frozen)
	}
	lost, err := store.LostBlockRecords(ctx, operation.ID, frozen)
	if err != nil {
		return nil, err
	}
	entries := contradictions(operation, frozen, states, attempts, lost)
	if len(reconciliation.OwnedSubset(frozen, states).Blocks) == 0 && !mayHaveStartedNothing(operation) {
		entries = append(entries, "the apply records "+string(operation.State)+", yet no block of it started")
	}
	return entries, nil
}
