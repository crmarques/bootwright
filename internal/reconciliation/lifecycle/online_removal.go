package lifecycle

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// owningApply uses durable lineage, independent of mutable context names.
func owningApply(operation operationstore.Operation) string {
	if operation.Verb == reconciliation.Apply {
		return operation.ID
	}
	return operation.Source
}

// onlineRemoval follows frozen dependencies and capability opt-in, never kind alone.
type onlineRemoval struct {
	effects  map[string]bool
	machines map[string]bool
	retired  map[string]bool
}

func (s Service) onlineRemoval(plan reconciliation.Plan) onlineRemoval {
	if plan.Verb != reconciliation.Destroy {
		return onlineRemoval{effects: map[string]bool{}, machines: map[string]bool{}, retired: map[string]bool{}}
	}
	retired := s.retiredWithHosts(plan)
	for {
		boundary := s.deferral(plan, retired)
		boundary.retired = retired
		unretired := false
		for _, block := range plan.Blocks {
			if retired[block.ID] && s.hostsDeferred(block, plan, boundary.machines) {
				delete(retired, block.ID)
				unretired = true
			}
		}
		if !unretired {
			return boundary
		}
	}
}

func (s Service) hostsDeferred(block reconciliation.Block, plan reconciliation.Plan, deferred map[string]bool) bool {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	bound, binds := capability.(HostBound)
	if !ok || !binds {
		return false
	}
	hosts, err := bound.HostMachines(block)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(plan.Blocks, func(candidate reconciliation.Block) bool {
		return deferred[candidate.ID] && slices.Contains(hosts, candidate.Object)
	})
}

func (s Service) deferral(plan reconciliation.Plan, retired map[string]bool) onlineRemoval {
	boundary := onlineRemoval{effects: map[string]bool{}, machines: map[string]bool{}}
	for _, block := range plan.Blocks {
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !ok || retired[block.ID] {
			continue
		}
		if online, ok := capability.(OnlineRemoval); ok && online.RequiresOnlineRemoval(block) {
			boundary.effects[block.ID] = true
		}
	}
	// Dependency order makes one propagation pass cover all transitive dependents.
	after := map[string]bool{}
	for _, block := range plan.Blocks {
		after[block.ID] = boundary.effects[block.ID]
		for _, dependency := range block.Dependencies {
			after[block.ID] = after[block.ID] || after[dependency]
		}
		if block.Stage == reconciliation.StageMachines && after[block.ID] && !boundary.effects[block.ID] {
			capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
			if prober, ok := capability.(QuiescenceProber); ok && prober.ProbesQuiescence() {
				boundary.machines[block.ID] = true
			}
		}
	}
	return boundary
}

func (s Service) retiredWithHosts(plan reconciliation.Plan) map[string]bool {
	before := map[string]map[string]bool{}
	for _, block := range plan.Blocks {
		reach := map[string]bool{}
		for _, dependency := range block.Dependencies {
			reach[dependency] = true
			for id := range before[dependency] {
				reach[id] = true
			}
		}
		before[block.ID] = reach
	}
	removesHost := func(block reconciliation.Block, after, machine string) bool {
		if block.Kind != string(api.Machine) || block.Object != machine || !before[block.ID][after] {
			return false
		}
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		removal, removes := capability.(HostRemoval)
		return ok && removes && removal.RemovesHost(block)
	}
	retired := map[string]bool{}
	for _, block := range plan.Blocks {
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		bound, binds := capability.(HostBound)
		if !ok || !binds {
			continue
		}
		machines, err := bound.HostMachines(block)
		if err != nil || len(machines) == 0 {
			continue
		}
		retired[block.ID] = !slices.ContainsFunc(machines, func(machine string) bool {
			return !slices.ContainsFunc(plan.Blocks, func(candidate reconciliation.Block) bool { return removesHost(candidate, block.ID, machine) })
		})
	}
	return retired
}

func (b onlineRemoval) complete(states map[string]reconciliation.BlockState) bool {
	for id := range b.effects {
		if states[id] != reconciliation.BlockDone {
			return false
		}
	}
	return true
}

func (s Service) stopsAfterOnline(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []string {
	boundary := s.onlineRemoval(plan)
	var names []string
	for _, block := range plan.Blocks {
		if boundary.machines[block.ID] && states[block.ID] != reconciliation.BlockDone && !slices.Contains(names, block.Object) {
			names = append(names, block.Object)
		}
	}
	return names
}

// proveOnlineBoundary runs only once all online effects have durable absence
// proof and no adapter is in flight. Failure records no Machine attempt: its
// pending block and the completed online blocks are the resumable boundary.
func (c *scheduler) proveOnlineBoundary(ctx context.Context, block reconciliation.Block) bool {
	if c.operation.Verb != reconciliation.Destroy || !c.online.machines[block.ID] || unproved(c.states[block.ID]) || c.onlineProved {
		return true
	}
	if len(c.running) != 0 || !c.online.complete(c.states) {
		return false
	}
	skip := map[string]bool{}
	for _, candidate := range c.plan.Blocks {
		if !c.online.machines[candidate.ID] || c.states[candidate.ID] == reconciliation.BlockDone {
			skip[candidate.ID] = true
		}
	}
	err := c.service.proveQuiescent(ctx, c.store, c.tx.Identity().Name, c.approved, c.plan, c.material, owningApply(c.operation), skip)
	if err != nil {
		c.onlineBlocked = true
		c.causes[block.ID] = err
		return false
	}
	c.onlineProved = true
	return true
}
