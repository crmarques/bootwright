package lifecycle

import "github.com/crmarques/bootwright/internal/reconciliation"

// reachableEffects is the upper bound on attempts the selected apply can run.
// An unproved predecessor can settle by observation even outside the selection.
// Pending or failed predecessors can settle only by an admitted attempt.
func reachableEffects(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) (map[string]bool, map[string]string) {
	effects := make(map[string]bool, len(plan.Blocks))
	blockedBy := make(map[string]string, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == reconciliation.BlockDone {
			continue
		}
		if !selection.Admits(block) {
			if !unproved(state) {
				blockedBy[block.ID] = block.ID
			}
			continue
		}
		blocker := ""
		for _, dependency := range block.Dependencies {
			if blocker = blockedBy[dependency]; blocker != "" {
				break
			}
		}
		if blocker == "" {
			effects[block.ID] = true
		} else if !unproved(state) {
			blockedBy[block.ID] = blocker
		}
	}
	return effects, blockedBy
}

func authorizationPlan(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) reconciliation.Plan {
	if plan.Verb != reconciliation.Apply {
		return plan
	}
	effects, _ := reachableEffects(plan, states, selection)
	scoped := reconciliation.Plan{Verb: plan.Verb}
	for _, block := range plan.Blocks {
		if effects[block.ID] {
			scoped.Blocks = append(scoped.Blocks, block)
		}
	}
	return scoped
}

func (p operationPlan) authorizationPlan() reconciliation.Plan {
	return authorizationPlan(p.plan, p.states, p.selection)
}

// effectSchedule projects attempt ordering for display.
func effectSchedule(plan reconciliation.Plan, effects map[string]bool) reconciliation.Plan {
	selected := reconciliation.Plan{Verb: plan.Verb}
	for _, block := range plan.Blocks {
		if !effects[block.ID] {
			continue
		}
		dependencies := block.Dependencies
		block.Dependencies = nil
		for _, id := range dependencies {
			if effects[id] {
				block.Dependencies = append(block.Dependencies, id)
			}
		}
		selected.Blocks = append(selected.Blocks, block)
	}
	return selected
}
