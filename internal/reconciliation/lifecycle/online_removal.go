package lifecycle

import (
	"cmp"
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// onlineRemoval follows frozen dependencies and capability opt-in, never kind alone.
type onlineRemoval struct {
	effects         map[string]bool
	machines        map[string]bool
	retired         map[string]bool
	preStop         map[string]bool
	preStopMachines map[string]bool
	survivors       map[string][]SurvivingConsumer
}

func (s Service) onlineRemoval(plan reconciliation.Plan) onlineRemoval {
	if plan.Verb != reconciliation.Destroy {
		return onlineRemoval{effects: map[string]bool{}, machines: map[string]bool{}, retired: map[string]bool{}, preStop: map[string]bool{}, preStopMachines: map[string]bool{}, survivors: map[string][]SurvivingConsumer{}}
	}
	retired, consumerHosts, survivors := s.retiredWithHosts(plan)
	for {
		boundary := s.deferral(plan, retired)
		boundary.retired, boundary.survivors = retired, survivors
		unretired := false
		for _, block := range plan.Blocks {
			if retired[block.ID] && (s.hostsDeferred(block, plan, boundary.machines) || machinesDeferred(consumerHosts[block.ID], plan, boundary.machines)) {
				delete(retired, block.ID)
				unretired = true
			}
		}
		if !unretired {
			s.preStopRemoval(plan, &boundary)
			return boundary
		}
	}
}

func (s Service) preStopRemoval(plan reconciliation.Plan, boundary *onlineRemoval) {
	boundary.preStop, boundary.preStopMachines = map[string]bool{}, map[string]bool{}
	for _, block := range plan.Blocks {
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if removal, opts := capability.(PreStopRemoval); ok && opts && removal.RequiresPreStopRemoval(block) {
			boundary.preStop[block.ID] = true
		}
	}
	after := map[string]bool{}
	for _, block := range plan.Blocks {
		after[block.ID] = boundary.preStop[block.ID]
		for _, dependency := range block.Dependencies {
			after[block.ID] = after[block.ID] || after[dependency]
		}
		if block.Stage != reconciliation.StageMachines || !after[block.ID] || boundary.preStop[block.ID] || boundary.machines[block.ID] {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		if prober, ok := capability.(QuiescenceProber); ok && prober.ProbesQuiescence() {
			boundary.preStopMachines[block.ID] = true
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
	return machinesDeferred(hosts, plan, deferred)
}

func machinesDeferred(hosts []string, plan reconciliation.Plan, deferred map[string]bool) bool {
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

func (s Service) retiredWithHosts(plan reconciliation.Plan) (map[string]bool, map[string][]string, map[string][]SurvivingConsumer) {
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
	consumerHosts, direct := s.consumers(plan, retired, before)
	survivors := map[string][]SurvivingConsumer{}
	for _, block := range plan.Blocks {
		if _, bound := retired[block.ID]; !bound {
			continue
		}
		found := slices.Clone(direct[block.ID])
		for _, earlier := range plan.Blocks {
			if before[block.ID][earlier.ID] {
				found = append(found, direct[earlier.ID]...)
			}
		}
		if len(found) == 0 {
			continue
		}
		slices.SortFunc(found, compareSurvivors)
		survivors[block.ID] = slices.Compact(found)
		retired[block.ID] = false
	}
	return retired, consumerHosts, survivors
}

func (s Service) consumers(plan reconciliation.Plan, bound map[string]bool, before map[string]map[string]bool) (map[string][]string, map[string][]SurvivingConsumer) {
	removed := func(machine string) bool {
		return slices.ContainsFunc(plan.Blocks, func(candidate reconciliation.Block) bool {
			if candidate.Kind != string(api.Machine) || candidate.Object != machine {
				return false
			}
			capability, ok := s.capabilities.Resolve(candidate.Kind, candidate.Implementation)
			deletion, deletes := capability.(MachineDeletion)
			return ok && deletes && deletion.DeletesMachine(candidate)
		})
	}
	hosts, survivors := map[string][]string{}, map[string][]SurvivingConsumer{}
	for _, consumer := range plan.Blocks {
		capability, ok := s.capabilities.Resolve(consumer.Kind, consumer.Implementation)
		reader, reads := capability.(ProducedInputConsumer)
		if !ok || !reads {
			continue
		}
		own := reconciliation.ObjectRef{Kind: consumer.Kind, Object: consumer.Object}
		inputs, err := reader.ProducedInputs(consumer)
		if err != nil {
			for _, provider := range plan.Blocks {
				if _, isBound := bound[provider.ID]; isBound && before[provider.ID][consumer.ID] {
					survivors[provider.ID] = append(survivors[provider.ID], SurvivingConsumer{Consumer: own, Provider: reconciliation.ObjectRef{Kind: provider.Kind, Object: provider.Object}})
				}
			}
			continue
		}
		home, machines := s.home(plan, consumer, capability)
		leaves := len(machines) != 0 && !slices.ContainsFunc(machines, func(machine string) bool { return !removed(machine) })
		for _, input := range inputs {
			for _, provider := range plan.Blocks {
				if _, isBound := bound[provider.ID]; !isBound || provider.Kind != input.Kind || provider.Object != input.Object {
					continue
				}
				if !leaves {
					survivors[provider.ID] = append(survivors[provider.ID], SurvivingConsumer{Consumer: home, Provider: reconciliation.ObjectRef{Kind: provider.Kind, Object: provider.Object}})
					continue
				}
				hosts[provider.ID] = append(hosts[provider.ID], machines...)
			}
		}
	}
	for id := range hosts {
		slices.Sort(hosts[id])
		hosts[id] = slices.Compact(hosts[id])
	}
	return hosts, survivors
}

func (s Service) home(plan reconciliation.Plan, consumer reconciliation.Block, capability Capability) (reconciliation.ObjectRef, []string) {
	own := reconciliation.ObjectRef{Kind: consumer.Kind, Object: consumer.Object}
	hosted, ok := capability.(HostedConsumer)
	if !ok {
		return own, nil
	}
	home, err := hosted.HostObject(consumer)
	if err != nil || home.Kind == "" || home.Object == "" {
		return own, nil
	}
	machines := []string{}
	for _, block := range plan.Blocks {
		if block.Kind != home.Kind || block.Object != home.Object {
			continue
		}
		resolved, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		nodes, binds := resolved.(NodeBound)
		if !ok || !binds {
			continue
		}
		found, err := nodes.NodeMachines(block)
		if err != nil || len(found) == 0 {
			return home, nil
		}
		machines = append(machines, found...)
	}
	slices.Sort(machines)
	return home, slices.Compact(machines)
}

const survivingConsumerRemedy = "keep this context while the consumer runs; a destroy takes back what a consumer uses only when the consumer leaves with owned virtual node Machines the same destroy deletes"

func survivorRefusal(blocks []reconciliation.Block, survivors map[string][]SurvivingConsumer) error {
	var consumers []string
	retired := map[string][]string{}
	for _, block := range blocks {
		found := survivors[block.ID]
		if len(found) == 0 {
			continue
		}
		named, object := consumption(found), block.Kind+"/"+block.Object
		if !slices.Contains(consumers, named) {
			consumers = append(consumers, named)
		}
		if !slices.Contains(retired[named], object) {
			retired[named] = append(retired[named], object)
		}
	}
	if len(consumers) == 0 {
		return nil
	}
	for index, named := range consumers {
		consumers[index] = named + " and survives this destroy, which takes back " + strings.Join(retired[named], ", ")
	}
	return failure("lifecycle.live", "this removal would take back state that is still in use: "+strings.Join(consumers, "; "), survivingConsumerRemedy)
}

func consumption(survivors []SurvivingConsumer) string {
	named := make([]string, 0, len(survivors))
	for _, survivor := range survivors {
		named = append(named, survivor.Consumer.Kind+"/"+survivor.Consumer.Object+" consumes "+survivor.Provider.Kind+"/"+survivor.Provider.Object)
	}
	return strings.Join(named, ", ")
}

func (s Service) removalWarnings(plan reconciliation.Plan) []diagnostics.Diagnostic {
	if plan.Verb != reconciliation.Apply {
		return nil
	}
	inverse, err := plan.Inverse()
	if err != nil {
		return nil
	}
	_, _, survivors := s.retiredWithHosts(inverse)
	var found []SurvivingConsumer
	for _, block := range inverse.Blocks {
		found = append(found, survivors[block.ID]...)
	}
	slices.SortFunc(found, compareSurvivors)
	var warnings []diagnostics.Diagnostic
	for _, survivor := range slices.Compact(found) {
		warnings = append(warnings, diagnostics.Diagnostic{Severity: "warning", Code: "lifecycle.surviving-consumer",
			Message:     "a destroy of this context refuses before registration while " + consumption([]SurvivingConsumer{survivor}) + " and outlives that destroy",
			Remediation: "run the consumer on owned virtual node Machines this context deletes, or keep this context while the consumer runs"})
	}
	return warnings
}

func compareSurvivors(a, b SurvivingConsumer) int {
	return cmp.Or(strings.Compare(a.Consumer.Kind, b.Consumer.Kind), strings.Compare(a.Consumer.Object, b.Consumer.Object), strings.Compare(a.Provider.Kind, b.Provider.Kind), strings.Compare(a.Provider.Object, b.Provider.Object))
}

func (b onlineRemoval) complete(states map[string]reconciliation.BlockState) bool {
	for id := range b.effects {
		if states[id] != reconciliation.BlockDone {
			return false
		}
	}
	return true
}

func (b onlineRemoval) preStopDone(states map[string]reconciliation.BlockState) bool {
	for id := range b.preStop {
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
		if (boundary.machines[block.ID] || boundary.preStopMachines[block.ID]) && states[block.ID] != reconciliation.BlockDone && !slices.Contains(names, block.Object) {
			names = append(names, block.Object)
		}
	}
	return names
}

// proveOnlineBoundary runs only once all online effects have durable absence proof and no adapter is
// in flight. Failure records no Machine attempt: its pending block and the completed online blocks resume.
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
	held, err := appliedLineage(ctx, c.store, c.operation)
	if err == nil {
		err = c.service.proveQuiescent(ctx, c.store, c.tx.Identity().Name, c.approved, c.plan, c.material, held, skip)
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
	if c.operation.Verb != reconciliation.Destroy || c.preStopProved || c.online.preStop[block.ID] || unproved(c.states[block.ID]) {
		return true
	}
	skip, pending := map[string]bool{}, false
	for _, candidate := range c.plan.Blocks {
		if !c.online.preStopMachines[candidate.ID] || c.states[candidate.ID] == reconciliation.BlockDone {
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
		err = c.service.proveQuiescent(ctx, c.store, c.tx.Identity().Name, c.approved, c.plan, c.material, held, skip)
	}
	if err != nil {
		c.onlineBlocked = true
		c.causes[block.ID] = err
		return false
	}
	c.preStopProved = true
	return true
}
