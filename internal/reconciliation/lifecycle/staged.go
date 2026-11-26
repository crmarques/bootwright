package lifecycle

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func staged(operation operationstore.Operation) bool {
	return operation.Verb == reconciliation.Destroy && operation.Stage != nil
}

func trimmed(operation operationstore.Operation) bool {
	return staged(operation) && operation.State == reconciliation.OperationDone
}

func frozenFloor(operation operationstore.Operation) stagedFloor {
	if !staged(operation) {
		return stagedFloor{}
	}
	return floorOf(operation.Stage)
}

func (t operationPlan) staged() bool {
	return t.stage != nil || !t.fresh && staged(t.operation)
}

func (d decider) stagedDecision(ctx context.Context, view View, store decisionRecords, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (operationPlan, bool, error) {
	name := view.Identity().Name
	switch {
	case !d.floor.staged() && !staged(operation):
		return operationPlan{}, false, nil
	case staged(operation) && operation.State != reconciliation.OperationDone && d.floor.same(frozenFloor(operation)):
		return operationPlan{}, false, nil
	case staged(operation) && operation.State != reconciliation.OperationDone && d.floor.staged() && !d.floor.covers(frozenFloor(operation)):
		return operationPlan{}, true, stageChanged(name, operation, frozen, states, d.floor)
	case operation.Verb == reconciliation.Destroy && !staged(operation):
		if operation.State == reconciliation.OperationDone {
			return operationPlan{}, false, nil
		}
		return operationPlan{}, true, destroyStageChanged.Fail(
			"the unfinished destroy "+operation.ID+" takes back the whole context, which a staged destroy cannot narrow",
			"finish it with "+continuationCommand(name, operation, frozen, states))
	}
	live, err := d.liveOf(ctx, store, name, operation, frozen, states)
	if err != nil {
		return operationPlan{}, true, err
	}
	if !d.floor.staged() {
		decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, live.source, live.scope, false)
		if err != nil {
			return operationPlan{}, true, err
		}
		decided, err = d.widen(ctx, store, decided, operation, frozen, states)
		return decided, true, err
	}
	ids, answered, err := d.planner.stagedScope(ctx, name, d.floor, live)
	if err != nil {
		return operationPlan{}, true, err
	}
	if len(ids) == 0 {
		return settledDestroy(ctx, store, operationPlan{noop: true, keeps: true, verb: reconciliation.Destroy, operation: operation, plan: frozen, states: states}), true, nil
	}
	scope := live.scope
	if scope.owned, err = scope.owned.Retain(ids); err != nil {
		return operationPlan{}, true, err
	}
	scope.started, scope.keptHosts, scope.blanks = len(scope.owned.Blocks), answered.hosts, answered.blanks
	decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, live.source, scope, false)
	if err != nil {
		return operationPlan{}, true, err
	}
	if err := refuseLeavingOutside(name, decided.plan); err != nil {
		return operationPlan{}, true, err
	}
	taken := append(slices.Clone(ids), live.unstarted...)
	if staged(operation) {
		taken = append(taken, operation.Stage.TakenBack...)
	}
	slices.Sort(taken)
	decided.stage = d.floor.record(slices.Compact(taken), answered.power)
	decided.release, decided.kept = nil, keptOf(live.scope.owned, ids)
	decided.keptBlocks = slices.DeleteFunc(slices.Clone(live.scope.owned.Blocks), func(block reconciliation.Block) bool { return slices.Contains(ids, block.ID) })
	decided, err = d.widen(ctx, store, decided, operation, frozen, states)
	return decided, true, err
}

type widening struct {
	operation string
	from      stagedFloor
}

func (d decider) widen(ctx context.Context, store decisionRecords, decided operationPlan, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (operationPlan, error) {
	if !staged(operation) || operation.State == reconciliation.OperationDone {
		return decided, nil
	}
	decided.widens = &widening{operation: operation.ID, from: floorOf(operation.Stage)}
	return d.replacing(ctx, store, decided, operation, frozen, states)
}

func (t operationPlan) widened() string {
	if t.widens == nil {
		return ""
	}
	to := "the whole context"
	if floor := t.floor(); floor.staged() {
		to = floor.named()
	}
	return "which widens the unfinished staged destroy " + t.widens.operation + " from " + t.widens.from.named() + " to " + to
}

type liveState struct {
	chain     lineage
	source    string
	scope     removalScope
	states    map[string]reconciliation.BlockState
	unstarted []string
	whole     reconciliation.Plan
}

func (d decider) liveOf(ctx context.Context, store decisionRecords, name string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (liveState, error) {
	if operation.Verb == reconciliation.Apply {
		owned := frozen
		if operation.State != reconciliation.OperationDone {
			owned = reconciliation.OwnedSubset(frozen, states)
			if len(owned.Blocks) != 0 {
				attempts := map[string]int{}
				for _, block := range frozen.Blocks {
					record, err := store.Block(ctx, operation.ID, block.ID)
					if err != nil {
						return liveState{}, err
					}
					attempts[block.ID] = record.Attempts
				}
				if err := refuseContradictions(ctx, store, name, operation, frozen, states, attempts); err != nil {
					return liveState{}, err
				}
			}
		}
		scope, err := removalOver(ctx, store, operation, frozen, owned)
		if err != nil {
			return liveState{}, err
		}
		held, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
		if err != nil {
			return liveState{}, err
		}
		whole, err := held.plan()
		if err != nil {
			return liveState{}, err
		}
		return liveState{chain: held, source: operation.ID, scope: scope, states: held.states(), unstarted: held.unstarted(), whole: whole}, nil
	}
	head, err := store.ReadOperation(ctx, operation.Source)
	if err != nil {
		return liveState{}, err
	}
	if head.Verb != reconciliation.Apply {
		return liveState{}, notAnApply()
	}
	headPlan, err := store.ReadPlan(ctx, head.ID)
	if err != nil {
		return liveState{}, err
	}
	headStates, err := store.BlockStates(ctx, head.ID, headPlan)
	if err != nil {
		return liveState{}, err
	}
	owned := headPlan
	if head.State != reconciliation.OperationDone {
		owned = reconciliation.OwnedSubset(headPlan, headStates)
	}
	scope, err := removalOver(ctx, store, head, headPlan, owned)
	if err != nil {
		return liveState{}, err
	}
	folded, err := removalLineage(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return liveState{}, err
	}
	live, held := folded.live()
	if scope.owned, err = scope.owned.Retain(live); err != nil {
		return liveState{}, err
	}
	scope.started = len(scope.owned.Blocks)
	scope.presents, scope.inherited = folded.presenting(), folded.replacedRebounds()
	scope.release = slices.Clone(scope.release)
	for _, member := range folded {
		if member.operation.Verb == reconciliation.Destroy {
			scope.release = joinBindings(scope.release, member.operation.Bindings...)
		}
	}
	return liveState{chain: folded, source: head.ID, scope: scope, states: held, whole: scope.owned}, nil
}

func keptOf(live reconciliation.Plan, scope []string) []string {
	var kept []string
	for _, block := range live.Blocks {
		if !slices.Contains(scope, block.ID) {
			kept = append(kept, block.ID)
		}
	}
	return kept
}

func (s Service) stagedScope(ctx context.Context, name string, floor stagedFloor, held liveState) ([]string, keptAnswer, error) {
	live, states := held.scope.owned, held.states
	if err := refuseClusters(name, floor, held); err != nil {
		return nil, keptAnswer{}, err
	}
	scope := s.closure(live, floor)
	if foreign, wider := s.foreignClusters(live, floor, scope); len(foreign) != 0 {
		return nil, keptAnswer{}, destroyStageOtherCluster.Fail(
			"these steps of clusters a staged destroy from "+floor.named()+" does not name depend on what it takes back: "+strings.Join(foreign, ", "),
			"take back their clusters too with "+contextCommand(name, "destroy --dry-run", wider.flags()...)+", or take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
	}
	if below := belowFloor(live, scope, floor.stage); len(below) != 0 {
		lower := s.widened(live, floor)
		remedy := "take back the whole context with " + contextCommand(name, "destroy --dry-run")
		if reconciliation.ValidFloor(lower.stage) {
			remedy = "take back from a lower stage with " + contextCommand(name, "destroy --dry-run", lower.flags()...)
		}
		return nil, keptAnswer{}, destroyStageBelowFloor.Fail(
			"these steps below "+floor.named()+" depend on what a staged destroy from it takes back: "+strings.Join(below, ", "), remedy)
	}
	var unfinished []string
	for _, block := range live.Blocks {
		if state := states[block.ID]; !slices.Contains(scope, block.ID) && state != reconciliation.BlockDone {
			if state == "" {
				state = reconciliation.BlockPending
			}
			unfinished = append(unfinished, block.ID+" ("+string(state)+")")
		}
	}
	if len(unfinished) != 0 {
		named := ""
		if wider := floor.joined(stagedFloor{stage: floor.stage, clusters: keptOwners(live, scope, states)}); len(wider.clusters) != 0 && !wider.same(floor) {
			named = ", take them back by naming their clusters too with " + contextCommand(name, "destroy --dry-run", wider.flags()...)
		}
		return nil, keptAnswer{}, destroyStageKeepsUnfinished.Fail(
			"a staged destroy from "+floor.named()+" would keep these steps, which are not done: "+strings.Join(unfinished, ", "),
			"finish them with "+contextCommand(name, "apply")+named+", take them back with a lower stage, or take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
	}
	if len(scope) == 0 {
		return scope, keptAnswer{}, nil
	}
	answered, err := s.keptHosts(ctx, name, floor, live, held.whole, scope, s.unstartedWith(held, floor, scope), held.scope.records)
	return scope, answered, err
}

func (s Service) unstartedWith(held liveState, floor stagedFloor, scope []string) []string {
	if len(floor.clusters) == 0 {
		return held.unstarted
	}
	reached := s.closureFrom(held.whole, func(block reconciliation.Block) bool {
		return floor.seeds(held.whole, block) || slices.Contains(scope, block.ID)
	})
	return slices.DeleteFunc(slices.Clone(held.unstarted), func(id string) bool { return !slices.Contains(reached, id) })
}

func keptOwners(live reconciliation.Plan, scope []string, states map[string]reconciliation.BlockState) []string {
	var owners []string
	for _, block := range live.Blocks {
		if !slices.Contains(scope, block.ID) && states[block.ID] != reconciliation.BlockDone {
			owners = append(owners, reconciliation.ClusterOwners(live, block)...)
		}
	}
	return owners
}

func (s Service) widened(live reconciliation.Plan, floor stagedFloor) stagedFloor {
	lower := floor
	for scope := s.closure(live, floor); ; {
		next := lowestStage(live, belowFloor(live, scope, lower.stage))
		if next == "" || !reconciliation.ValidFloor(next) {
			if next != "" {
				lower = lower.at(next)
			}
			return lower
		}
		lower = lower.at(next)
		scope = s.closure(live, lower)
	}
}

func (s Service) closure(live reconciliation.Plan, floor stagedFloor) []string {
	return s.closureFrom(live, func(block reconciliation.Block) bool { return floor.seeds(live, block) })
}

func (s Service) closureFrom(live reconciliation.Plan, seeds func(reconciliation.Block) bool) []string {
	in := map[string]bool{}
	for _, block := range live.Blocks {
		if seeds(block) {
			in[block.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, block := range live.Blocks {
			if !in[block.ID] && (slices.ContainsFunc(block.Dependencies, func(id string) bool { return in[id] }) || s.consumesFrom(live, block, in)) {
				in[block.ID], changed = true, true
			}
		}
	}
	var scope []string
	for _, block := range live.Blocks {
		if in[block.ID] {
			scope = append(scope, block.ID)
		}
	}
	return scope
}

func (s Service) consumesFrom(live reconciliation.Plan, consumer reconciliation.Block, in map[string]bool) bool {
	capability, ok := s.capabilities.Resolve(consumer.Kind, consumer.Implementation)
	reader, reads := capability.(ProducedInputConsumer)
	if !ok || !reads {
		return false
	}
	inputs, err := reader.ProducedInputs(consumer)
	if err != nil {
		return slices.ContainsFunc(live.Blocks, func(provider reconciliation.Block) bool { return in[provider.ID] })
	}
	return slices.ContainsFunc(inputs, func(input ProducedInput) bool {
		return slices.ContainsFunc(live.Blocks, func(provider reconciliation.Block) bool {
			return in[provider.ID] && provider.Kind == input.Kind && provider.Object == input.Object
		})
	})
}

func belowFloor(live reconciliation.Plan, scope []string, floor reconciliation.Stage) []string {
	var below []string
	for _, block := range live.Blocks {
		if slices.Contains(scope, block.ID) && !block.Stage.AtOrAbove(floor) {
			below = append(below, block.ID)
		}
	}
	return below
}

func lowestStage(live reconciliation.Plan, ids []string) reconciliation.Stage {
	lowest := reconciliation.Stage("")
	for _, block := range live.Blocks {
		if slices.Contains(ids, block.ID) && (lowest == "" || !block.Stage.AtOrAbove(lowest)) {
			lowest = block.Stage
		}
	}
	return lowest
}

type keptAnswer struct {
	hosts, blanks map[string][]string
	online        map[string]bool
	power         []operationstore.KeptPower
}

func (s Service) keptHosts(ctx context.Context, name string, floor stagedFloor, live, whole reconciliation.Plan, scope, unstarted []string, records func(context.Context, string) (RemovalRecords, error)) (keptAnswer, error) {
	fleet := s.fleetOf(live, scope)
	managed, deleted, virtual, released := fleet.managed, fleet.deleted, fleet.virtual, fleet.released
	steps, bound := slices.Concat(scope, unstarted), map[string][]string{}
	for _, id := range steps {
		block, _ := whole.Block(id)
		machines := s.boundMachines(whole, block)
		slices.Sort(machines)
		bound[id] = slices.DeleteFunc(slices.Compact(machines), func(machine string) bool { return !managed[machine] || deleted[machine] })
	}
	answer, settled := keptAnswer{hosts: map[string][]string{}, blanks: map[string][]string{}, online: map[string]bool{}}, map[string]bool{}
	readOf := func(id string) (RemovalRecords, error) {
		if !slices.Contains(scope, id) || records == nil {
			return RemovalRecords{State: reconciliation.BlockPending}, nil
		}
		return records(ctx, id)
	}
	for _, id := range steps {
		block, _ := whole.Block(id)
		kept, keeps := s.capabilityOf(block).(KeptHostRemoval)
		named, online, err := s.keptNamed(whole, block, keeps)
		if err != nil {
			return keptAnswer{}, err
		}
		answer.online[id] = online
		hosts := slices.DeleteFunc(slices.Clone(bound[id]), func(machine string) bool { return released[machine] })
		if keeps && len(bound[id]) != 0 && len(hosts) == 0 {
			if err := releasedKept(name, floor, block, kept, bound[id], online, virtual, readOf); err != nil {
				return keptAnswer{}, err
			}
			settled[id] = true
			continue
		}
		if len(hosts) == 0 || !keeps || slices.ContainsFunc(hosts, func(machine string) bool { return !slices.Contains(named, machine) }) {
			continue
		}
		read, err := readOf(id)
		if err != nil {
			return keptAnswer{}, err
		}
		if refusal := kept.KeptHostsRefusal(block, read, false); refusal != "" && answer.online[id] {
			return keptAnswer{}, onlineRefusal(name, floor, id, hosts, refusal, read, virtual)
		} else if refusal != "" {
			physical := slices.ContainsFunc(hosts, func(machine string) bool { return !virtual[machine] })
			blank := ""
			if !physical {
				blank = s.unblankable(whole, steps, hosts)
			}
			if physical || blank != "" || kept.KeptHostsRefusal(block, read, true) != "" {
				return keptAnswer{}, keptHostRefusal(name, floor.stage, id, hosts, refusal, blank, physical)
			}
			answer.blanks[id] = hosts
		}
		answer.hosts[id] = hosts
	}
	for _, id := range steps {
		block, _ := whole.Block(id)
		if settled[id] || answer.hosts[id] != nil {
			continue
		}
		machines := slices.DeleteFunc(slices.Clone(bound[id]), func(machine string) bool { return released[machine] })
		if len(machines) == 0 {
			if len(bound[id]) == 0 {
				continue
			}
			if err := s.rereadable(ctx, name, block, slices.Contains(scope, id), records); err != nil {
				return keptAnswer{}, err
			}
			continue
		}
		if s.retiresWith(whole, block, answer.hosts, machines) {
			continue
		}
		return keptAnswer{}, fleet.unkept(name, floor, live, scope, id, machines, onlineHolder(answer, machines))
	}
	return answer, s.keptPower(name, whole, &answer)
}

func (s Service) keptNamed(whole reconciliation.Plan, block reconciliation.Block, keeps bool) ([]string, bool, error) {
	named := s.namedMachines(whole, block)
	online, runs := s.capabilityOf(block).(OnlineKeptHosts)
	if !keeps || !runs {
		return named, false, nil
	}
	hosts, err := online.OnlineKeptHosts(block)
	return append(named, hosts...), true, err
}

func releasedKept(name string, floor stagedFloor, block reconciliation.Block, kept KeptHostRemoval, machines []string, online bool, virtual map[string]bool, readOf func(string) (RemovalRecords, error)) error {
	read, err := readOf(block.ID)
	if err != nil {
		return err
	}
	refusal := kept.KeptHostsRefusal(block, read, false)
	switch {
	case refusal != "" && online:
		return onlineRefusal(name, floor, block.ID, machines, refusal, read, virtual)
	case refusal != "":
		return keptHostRefusal(name, floor.stage, block.ID, machines, refusal, "", true)
	}
	return nil
}

func onlineRefusal(name string, floor stagedFloor, id string, machines []string, refusal string, read RemovalRecords, virtual map[string]bool) error {
	remedy := "take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
	if !slices.ContainsFunc(machines, func(machine string) bool { return !virtual[machine] }) {
		remedy = "take back from the machines stage with " + contextCommand(name, "destroy --dry-run", "--stage", string(reconciliation.StageMachines)) + ", which deletes them, or " + remedy
	}
	if read.State == reconciliation.BlockUnknown || read.State == reconciliation.BlockRunning {
		remedy = "resolve the step with " + contextCommand(name, string(reconciliation.Apply)) + ", then repeat " +
			contextCommand(name, string(reconciliation.Destroy), invocationFlags(floor, reconciliation.Selection{}, nil)...) + ", or " + remedy
	}
	return destroyStageKeepsHost.Fail("the step "+id+" runs on Machine/"+strings.Join(machines, ", Machine/")+", which stay below the "+string(floor.stage)+" stage, and "+refusal, remedy)
}

func onlineHolder(answer keptAnswer, machines []string) string {
	for _, id := range slices.Sorted(maps.Keys(answer.hosts)) {
		if answer.online[id] && slices.ContainsFunc(machines, func(machine string) bool { return slices.Contains(answer.hosts[id], machine) }) {
			return id
		}
	}
	return ""
}

type keptFleet struct {
	managed, deleted, virtual, released, claimed map[string]bool
}

func (s Service) fleetOf(live reconciliation.Plan, scope []string) keptFleet {
	fleet := keptFleet{managed: map[string]bool{}, deleted: map[string]bool{}, virtual: map[string]bool{}, released: map[string]bool{}, claimed: map[string]bool{}}
	for _, block := range live.Blocks {
		if block.Kind != string(api.Machine) {
			continue
		}
		fleet.managed[block.Object] = true
		if deletion, deletes := s.capabilityOf(block).(MachineDeletion); deletes && deletion.DeletesMachine(block) {
			fleet.virtual[block.Object] = true
			fleet.deleted[block.Object] = fleet.deleted[block.Object] || slices.Contains(scope, block.ID)
		}
		if release, releases := s.capabilityOf(block).(MachineRelease); releases && release.ReleasesMachine(block) {
			fleet.claimed[block.Object] = true
			fleet.released[block.Object] = fleet.released[block.Object] || slices.Contains(scope, block.ID)
		}
	}
	return fleet
}

func (f keptFleet) unkept(name string, floor stagedFloor, live reconciliation.Plan, scope []string, id string, machines []string, holder string) error {
	reason := "these Machines stay below the " + string(floor.stage) + " stage, and no staged destroy takes such a step back on a Machine it keeps"
	remedy := "take back from the machines stage with " + contextCommand(name, "destroy --dry-run", "--stage", string(reconciliation.StageMachines)) +
		", or take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
	switch {
	case holder != "":
		reason = "the step " + holder + " is taken back from these Machines while they run, and no staged destroy takes back a step that runs beside it on them yet (B1885)"
		remedy = "take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
	case slices.ContainsFunc(machines, func(machine string) bool { return f.claimed[machine] && !f.released[machine] }):
		reason = "a physical Machine stays below the " + string(floor.stage) + " stage, and a staged destroy takes back a step on it only with the Machine's own step"
	case slices.ContainsFunc(machines, func(machine string) bool { return physicalInScope(live, scope, machine) }):
		reason = "a physical Machine is always kept unless its own step only releases it, and no staged destroy takes back a step on it otherwise"
		remedy = "take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
	}
	return destroyStageKeepsHost.Fail("the step "+id+" runs on Machine/"+strings.Join(machines, ", Machine/")+": "+reason, remedy)
}

func (s Service) rereadable(ctx context.Context, name string, block reconciliation.Block, scoped bool, records func(context.Context, string) (RemovalRecords, error)) error {
	reader, reads := s.capabilityOf(block).(DiskRereading)
	if !reads {
		return nil
	}
	read := RemovalRecords{State: reconciliation.BlockPending}
	if scoped && records != nil {
		var err error
		if read, err = records(ctx, block.ID); err != nil {
			return err
		}
	}
	if refusal := reader.RereadRefusal(block, read); refusal != "" {
		return destroyStageKeepsHost.Fail("the step "+block.ID+" read the disks of Machine/"+block.Object+", which this staged destroy releases, and "+refusal+", so nothing proves the restore can read them again",
			"take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
	}
	return nil
}

func keptHostRefusal(name string, floor reconciliation.Stage, id string, machines []string, refusal, blank string, physical bool) error {
	message := "the step " + id + " runs on Machine/" + strings.Join(machines, ", Machine/") + ", which stay below the " + string(floor) + " stage, and " + refusal
	if physical {
		return destroyStageKeepsHost.Fail(message+"; a physical Machine keeps what was written to its disks, and no step of this context blanks a node's disks",
			"take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
	}
	return destroyStageKeepsHost.Fail(message+", and the removal cannot blank their root disks: "+blank,
		"take back from the machines stage with "+contextCommand(name, "destroy --dry-run", "--stage", string(reconciliation.StageMachines))+
			", or take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
}

func (s Service) unblankable(whole reconciliation.Plan, steps, machines []string) string {
	for _, machine := range machines {
		var reset RootDiskReset
		for _, block := range whole.Blocks {
			if candidate, resets := s.capabilityOf(block).(RootDiskReset); resets && block.Kind == string(api.Machine) && block.Object == machine {
				if refusal := candidate.RootResetRefusal(block); refusal != "" {
					return "Machine/" + machine + " " + refusal
				}
				reset = candidate
			}
		}
		if reset == nil {
			return "the substrate of Machine/" + machine + " blanks no root disk yet (B1880)"
		}
		for _, block := range whole.Blocks {
			deletion, deletes := s.capabilityOf(block).(MachineDeletion)
			own := block.Kind == string(api.Machine) && block.Object == machine && deletes && deletion.DeletesMachine(block)
			if !slices.Contains(steps, block.ID) && !own && slices.Contains(s.boundMachines(whole, block), machine) {
				return "the step " + block.ID + ", which stays, lives on Machine/" + machine
			}
		}
	}
	return ""
}

func (s Service) namedMachines(whole reconciliation.Plan, block reconciliation.Block) []string {
	var machines []string
	for _, id := range block.Dependencies {
		if dependency, found := whole.Block(id); found && dependency.Kind == string(api.Machine) {
			machines = append(machines, dependency.Object)
		}
	}
	if bound, binds := s.capabilityOf(block).(HostBound); binds {
		hosts, _ := bound.HostMachines(block)
		machines = append(machines, hosts...)
	}
	if bound, binds := s.capabilityOf(block).(NodeBound); binds {
		nodes, _ := bound.NodeMachines(block)
		machines = append(machines, nodes...)
	}
	return machines
}

func (s Service) retiresWith(whole reconciliation.Plan, block reconciliation.Block, answered map[string][]string, machines []string) bool {
	retirement, retires := s.capabilityOf(block).(OfflineRetirement)
	if !retires || !retirement.RetiresOffline() {
		return false
	}
	var covered []string
	seen, pending := map[string]bool{}, slices.Clone(block.Dependencies)
	for len(pending) != 0 {
		id := pending[0]
		pending = pending[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		covered = append(covered, answered[id]...)
		if dependency, found := whole.Block(id); found {
			pending = append(pending, dependency.Dependencies...)
		}
	}
	return !slices.ContainsFunc(machines, func(machine string) bool { return !slices.Contains(covered, machine) })
}

func (s Service) keptPower(name string, whole reconciliation.Plan, answer *keptAnswer) error {
	needs, by := map[string]string{}, map[string]string{}
	var machines []string
	for _, id := range slices.Sorted(maps.Keys(answer.hosts)) {
		power := operationstore.PoweredOff
		if answer.online[id] {
			power = operationstore.PoweredOn
		}
		for _, machine := range answer.hosts[id] {
			if needs[machine] != "" && needs[machine] != power {
				return destroyStageKeepsHost.Fail("the step "+by[machine]+" is taken back while Machine/"+machine+" is "+powerWord(needs[machine])+
					", and the step "+id+" only while it is "+powerWord(power), "take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
			}
			needs[machine], by[machine] = power, id
			machines = append(machines, machine)
		}
	}
	slices.Sort(machines)
	bindings := s.capabilities.Bindings()
	for _, machine := range slices.Compact(machines) {
		found := false
		for _, block := range whole.Blocks {
			if block.Kind == string(api.Machine) && block.Object == machine && managedPowerTarget(bindings, block) {
				answer.power, found = append(answer.power, operationstore.KeptPower{Block: block.ID, Machine: machine, Power: needs[machine]}), true
			}
		}
		if !found && needs[machine] == operationstore.PoweredOn {
			at := slices.IndexFunc(whole.Blocks, func(block reconciliation.Block) bool {
				return block.Kind == string(api.Machine) && block.Object == machine
			})
			if at >= 0 {
				answer.power, found = append(answer.power, operationstore.KeptPower{Block: whole.Blocks[at].ID, Machine: machine, Power: operationstore.Reachable}), true
			}
		}
		if !found {
			return destroyStageKeepsHost.Fail("no step of this context can prove the kept Machine/"+machine+" "+powerWord(needs[machine]),
				"take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
		}
	}
	slices.SortFunc(answer.power, func(x, y operationstore.KeptPower) int { return strings.Compare(x.Block, y.Block) })
	return nil
}

func powerWord(power string) string {
	if power == operationstore.Reachable {
		return "reachable over its own connection"
	}
	return "powered " + power
}

func (s Service) boundMachines(live reconciliation.Plan, block reconciliation.Block) []string {
	var machines []string
	seen := map[string]bool{block.ID: true}
	pending := slices.Clone(block.Dependencies)
	for len(pending) != 0 {
		id := pending[0]
		pending = pending[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		dependency, found := live.Block(id)
		if !found {
			continue
		}
		if dependency.Kind == string(api.Machine) {
			machines = append(machines, dependency.Object)
		}
		pending = append(pending, dependency.Dependencies...)
	}
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return machines
	}
	if bound, binds := capability.(HostBound); binds {
		hosts, _ := bound.HostMachines(block)
		machines = append(machines, hosts...)
	}
	if bound, binds := capability.(NodeBound); binds {
		nodes, _ := bound.NodeMachines(block)
		machines = append(machines, nodes...)
	}
	return machines
}

func physicalInScope(live reconciliation.Plan, scope []string, machine string) bool {
	return slices.ContainsFunc(live.Blocks, func(block reconciliation.Block) bool {
		return block.Kind == string(api.Machine) && block.Object == machine && block.Stage == reconciliation.StageMachines && slices.Contains(scope, block.ID)
	})
}

func refuseLeavingOutside(name string, plan reconciliation.Plan) error {
	for _, block := range plan.Blocks {
		if block.Inverse == nil {
			continue
		}
		for _, witness := range block.Inverse.LeavesWith {
			if _, found := plan.Block(witness); !found {
				return destroyStageKeepsHost.Fail("the step "+block.ID+" leaves with "+witness+", which a staged destroy keeps",
					"take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
			}
		}
	}
	return nil
}

func stageChanged(name string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, floor stagedFloor) error {
	held := frozenFloor(operation)
	message := "the unfinished staged destroy " + operation.ID + " froze " + held.named() + ", not " + floor.named()
	widen := ""
	if covering := held.joined(floor); !covering.same(held) {
		widen = ", widen it with " + contextCommand(name, "destroy --dry-run", covering.flags()...)
	}
	return destroyStageChanged.Fail(message+", which does not cover it, so it cannot narrow it",
		"finish it with "+continuationCommand(name, operation, frozen, states)+widen+", or review taking back the whole context with "+contextCommand(name, "destroy --dry-run")+" and run "+contextCommand(name, string(reconciliation.Destroy)))
}

func refuseTrimmed(name string, operation operationstore.Operation) error {
	took := "the staged destroy " + operation.ID + " took back " + frozenFloor(operation).named() + " and everything built on it"
	return prepareRefused.Fail(took+", and only an apply restores it", "restore it with "+contextCommand(name, string(reconciliation.Apply))+" first")
}

func (s Service) withFloor(floor string, clusters []string, relaxations map[string]string) (Service, error) {
	if floor == "" && len(clusters) != 0 {
		return s, engineInputInvalid.Fail("--clusters narrows only a staged destroy", "repeat with --stage clusters or --stage add-ons, or destroy without --clusters")
	}
	if floor == "" {
		return s, nil
	}
	if !reconciliation.ValidFloor(reconciliation.Stage(floor)) {
		return s, engineInputInvalid.Fail("--stage "+floor+" is no stage a destroy takes back from", "name one of substrates, machines, clusters or add-ons, or destroy without --stage")
	}
	if len(clusters) != 0 && !reconciliation.NarrowsToClusters(reconciliation.Stage(floor)) {
		return s, engineInputInvalid.Fail("--clusters narrows only the clusters or add-ons floor, and the "+floor+" stage holds steps no cluster owns", "repeat with --stage clusters or --stage add-ons, or without --clusters")
	}
	for _, cluster := range clusters {
		if !reconciliation.ValidSegment(cluster) {
			return s, engineInputInvalid.Fail("--clusters names must be lowercase DNS labels", "repeat naming each cluster by its name")
		}
	}
	for machine, method := range relaxations {
		if method == Abandon {
			return s, engineInputInvalid.Fail("--authorize abandon="+machine+" leaves steps in place, which a staged destroy never does", "repeat without abandon=, or destroy without --stage")
		}
	}
	s.floor = stagedFloor{stage: reconciliation.Stage(floor), clusters: slices.Compact(slices.Sorted(slices.Values(clusters)))}
	if len(s.floor.clusters) == 0 {
		s.floor.clusters = nil
	}
	return s, nil
}

func keptSteps(ctx context.Context, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) ([]string, error) {
	folded, err := removalLineage(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return nil, err
	}
	return folded.keptLive(), nil
}

func (l lineage) keptLive() []string {
	live, _ := l.live()
	kept := l.kept()
	return slices.DeleteFunc(live, func(id string) bool { return !kept[id] })
}

func (t operationPlan) floor() stagedFloor {
	switch {
	case t.verb != reconciliation.Destroy:
		return stagedFloor{}
	case t.stage != nil:
		return floorOf(t.stage)
	case t.requested.staged():
		return t.requested
	case staged(t.operation) && !t.fresh:
		return floorOf(t.operation.Stage)
	}
	return stagedFloor{}
}

func confirmedAction(decided operationPlan) string {
	floor := decided.floor()
	action := string(decided.verb)
	if decided.verb == reconciliation.Destroy && floor.staged() {
		action = "destroy " + strings.Join(floor.flags(), " ") + ", taking back " + strconv.Itoa(len(decided.plan.Blocks)) + " steps and keeping " + strconv.Itoa(len(decided.kept)) + ","
	}
	if widened := decided.widened(); widened != "" {
		action = strings.TrimSuffix(action, ",") + ", " + widened + ","
	}
	return action
}

func (t operationPlan) scope() *StagedScope {
	floor := t.floor()
	if t.verb != reconciliation.Destroy || !floor.staged() {
		return nil
	}
	scope := &StagedScope{Floor: string(floor.stage), Clusters: append([]string{}, floor.clusters...), Removed: []string{}, Kept: append([]string{}, t.kept...), Residue: []string{}, KeptPowerTargets: []string{}}
	stage := t.stage
	if stage == nil && !t.fresh && staged(t.operation) {
		stage = t.operation.Stage
	}
	if stage != nil {
		for _, target := range stage.KeptPower {
			scope.KeptPowerTargets = append(scope.KeptPowerTargets, "Machine/"+target.Machine+" "+powerWord(target.Power))
		}
	}
	for _, block := range t.plan.Blocks {
		scope.Removed = append(scope.Removed, block.ID)
		if block.Inverse != nil {
			for _, kept := range block.Inverse.Retains {
				scope.Residue = append(scope.Residue, block.ID+": "+kept)
			}
		}
	}
	return scope
}

type Widening struct {
	Operation    string
	From         string
	FromClusters []string
}

func (t operationPlan) widening() *Widening {
	if t.widens == nil {
		return nil
	}
	return &Widening{Operation: t.widens.operation, From: string(t.widens.from.stage), FromClusters: append([]string{}, t.widens.from.clusters...)}
}

type StagedScope struct {
	Floor            string
	Clusters         []string
	Removed          []string
	Kept             []string
	Residue          []string
	KeptPowerTargets []string
}

func (d decider) decideStaged(ctx context.Context, view View, store decisionRecords, verb reconciliation.Verb, selection reconciliation.Selection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (operationPlan, bool, error) {
	if verb == reconciliation.Apply {
		if err := d.admitPreparationCommand(frozen, operation); err != nil {
			return operationPlan{}, true, err
		}
		if trimmed(operation) && d.preparation != nil && d.preparation.Command != string(reconciliation.Apply) {
			return operationPlan{}, true, refuseTrimmed(view.Identity().Name, operation)
		}
		if trimmed(operation) {
			decided, err := d.restoring(ctx, view, store, operation, selection)
			return plannedFrom(decided, operation, states, attempts), true, err
		}
		return operationPlan{}, false, nil
	}
	decided, handled, err := d.stagedDecision(ctx, view, store, operation, frozen, states)
	if !handled || err != nil {
		return operationPlan{}, handled, err
	}
	decided.requested = d.floor
	return plannedFrom(decided, operation, states, attempts), true, nil
}

func keptOver(ctx context.Context, store recordReader, verb reconciliation.Verb, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) ([]string, error) {
	if verb != reconciliation.Destroy || !staged(operation) {
		return nil, nil
	}
	return keptSteps(ctx, store, operation, frozen, states)
}
