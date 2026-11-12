package lifecycle

import "github.com/crmarques/bootwright/internal/reconciliation"

// remainingChains measures selected unfinished paths in reverse topological order.
// Recompute after settlements so resolved effects no longer contribute priority.
func remainingChains(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) map[string]int {
	priority := make(map[string]int, len(plan.Blocks))
	for index := len(plan.Blocks) - 1; index >= 0; index-- {
		block := plan.Blocks[index]
		if states[block.ID] == reconciliation.BlockDone || !selection.Admits(block) {
			continue
		}
		priority[block.ID] = max(priority[block.ID], 1)
		for _, dependency := range block.Dependencies {
			priority[dependency] = max(priority[dependency], priority[block.ID]+1)
		}
	}
	return priority
}
