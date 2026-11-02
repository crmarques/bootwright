package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (b removalPhases) complete(states map[string]reconciliation.BlockState) bool {
	for id := range b.effects {
		if states[id] != reconciliation.BlockDone {
			return false
		}
	}
	return true
}

func (b removalPhases) preStopDone(states map[string]reconciliation.BlockState) bool {
	for id := range b.preStop {
		if states[id] != reconciliation.BlockDone {
			return false
		}
	}
	return true
}

// proveOnlineBoundary runs only once all online effects have durable absence proof and no adapter is
// in flight. Failure records no Machine attempt: its pending block and the completed online blocks resume.
func (c *scheduler) proveOnlineBoundary(ctx context.Context, block reconciliation.Block) bool {
	if c.operation.Verb != reconciliation.Destroy || !c.phases.machines[block.ID] || unproved(c.states[block.ID]) || c.onlineProved {
		return true
	}
	if len(c.running) != 0 || !c.phases.complete(c.states) {
		return false
	}
	skip := map[string]bool{}
	for _, candidate := range c.plan.Blocks {
		if !c.phases.machines[candidate.ID] || c.states[candidate.ID] == reconciliation.BlockDone {
			skip[candidate.ID] = true
		}
	}
	held, err := appliedLineage(ctx, c.store, c.operation)
	if err == nil {
		err = c.service.proveQuiescent(ctx, c.store, c.tx.Identity().Name, c.approved, c.plan, c.phases, c.material, held, skip)
	}
	if err != nil {
		c.onlineBlocked = true
		c.causes[block.ID] = err
		return false
	}
	c.onlineProved = true
	return true
}

func (c *scheduler) provePreStopBoundary(ctx context.Context, block reconciliation.Block) bool {
	if c.operation.Verb != reconciliation.Destroy || c.preStopProved || c.phases.preStop[block.ID] || unproved(c.states[block.ID]) {
		return true
	}
	skip, pending := map[string]bool{}, false
	for _, candidate := range c.plan.Blocks {
		if !c.phases.preStopMachines[candidate.ID] || c.states[candidate.ID] == reconciliation.BlockDone {
			skip[candidate.ID] = true
			continue
		}
		pending = true
	}
	if !pending {
		c.preStopProved = true
		return true
	}
	if len(c.running) != 0 {
		return false
	}
	held, err := appliedLineage(ctx, c.store, c.operation)
	if err == nil {
		err = c.service.proveQuiescent(ctx, c.store, c.tx.Identity().Name, c.approved, c.plan, c.phases, c.material, held, skip)
	}
	if err != nil {
		c.onlineBlocked = true
		c.causes[block.ID] = err
		return false
	}
	c.preStopProved = true
	return true
}
