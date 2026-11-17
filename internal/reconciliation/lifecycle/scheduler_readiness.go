package lifecycle

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type readiness struct {
	positions  map[string][]int
	dependents [][]int
	waiting    []int
}

func newReadiness(plan reconciliation.Plan, states map[string]reconciliation.BlockState) readiness {
	r := readiness{positions: make(map[string][]int, len(plan.Blocks)), dependents: make([][]int, len(plan.Blocks)), waiting: make([]int, len(plan.Blocks))}
	for index, block := range plan.Blocks {
		r.positions[block.ID] = append(r.positions[block.ID], index)
	}
	for index, block := range plan.Blocks {
		var seen []int
		for _, dependency := range block.Dependencies {
			for _, at := range r.positions[dependency] {
				if slices.Contains(seen, at) {
					continue
				}
				seen = append(seen, at)
				r.dependents[at] = append(r.dependents[at], index)
				if states[plan.Blocks[at].ID] != reconciliation.BlockDone {
					r.waiting[index]++
				}
			}
		}
	}
	return r
}

func (r readiness) moved(id string, from, to reconciliation.BlockState) {
	if (from == reconciliation.BlockDone) == (to == reconciliation.BlockDone) {
		return
	}
	step := 1
	if to == reconciliation.BlockDone {
		step = -1
	}
	for _, at := range r.positions[id] {
		for _, dependent := range r.dependents[at] {
			r.waiting[dependent] += step
		}
	}
}

func (c *scheduler) startable(index int, block reconciliation.Block) bool {
	state := c.states[block.ID]
	return c.readiness.waiting[index] == 0 && (state == "" || state == reconciliation.BlockPending) && c.selection.Admits(block)
}

func startableIn(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) func(int, reconciliation.Block) bool {
	var ready map[string]bool
	return func(_ int, block reconciliation.Block) bool {
		if ready == nil {
			ready = map[string]bool{}
			for _, startable := range reconciliation.Startable(plan, states, selection) {
				ready[startable.ID] = true
			}
		}
		return ready[block.ID]
	}
}
