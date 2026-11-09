package lifecycle

import (
	"context"
	"path"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// liveBlocks lists each running block in plan order with its newest attempt's
// output. It probes nothing; a block an interrupted invocation left running
// counts, and a log area that cannot be listed reads as not yet written.
func liveBlocks(ctx context.Context, store OperationStore, reference, id string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) ([]LiveBlock, error) {
	live := []LiveBlock{}
	for _, block := range plan.Blocks {
		if states[block.ID] != reconciliation.BlockRunning || attempts[block.ID] < 1 {
			continue
		}
		entry := LiveBlock{Block: block.ID, Attempt: attempts[block.ID]}
		output, found, err := store.LatestOutput(ctx, id, block.ID, entry.Attempt)
		if err != nil {
			found = false
		}
		if found {
			entry.Resolution, entry.Output, entry.Present, entry.Bytes = output.Resolution, path.Join(reference, output.Path), true, output.Size
		} else if entry.Output, err = pendingOutput(reference, id, block.ID, entry.Attempt); err != nil {
			return nil, err
		}
		live = append(live, entry)
	}
	return live, nil
}

func pendingOutput(reference, id, block string, attempt int) (string, error) {
	named, err := operationstore.AttemptLogPath(id, block, attempt, 0)
	if err != nil {
		return "", err
	}
	return path.Join(reference, named[:len(named)-len("jsonl")]+"output"), nil
}
