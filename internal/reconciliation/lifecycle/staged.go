package lifecycle

import (
	"context"
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

func frozenFloor(operation operationstore.Operation) reconciliation.Stage {
	if !staged(operation) {
		return ""
	}
	return operation.Stage.Floor
}

func (t operationPlan) staged() bool {
	return t.stage != nil || !t.fresh && staged(t.operation)
}

func (d decider) stagedDecision(ctx context.Context, view View, store decisionRecords, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (operationPlan, bool, error) {
	name := view.Identity().Name
	switch {
	case d.floor == "" && !staged(operation):
		return operationPlan{}, false, nil
	case staged(operation) && operation.State != reconciliation.OperationDone:
		if d.floor == operation.Stage.Floor {
			return operationPlan{}, false, nil
		}
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
	if d.floor == "" {
		decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, live.source, live.scope, false)
		return decided, true, err
	}
	ids, err := d.planner.stagedScope(name, d.floor, live)
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
	scope.started = len(scope.owned.Blocks)
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
	decided.stage = &operationstore.Stage{Floor: d.floor, TakenBack: slices.Compact(taken)}
	decided.release, decided.kept = nil, keptOf(live.scope.owned, ids)
	return decided, true, nil
}

type liveState struct {
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
		return liveState{source: operation.ID, scope: scope, states: held.states(), unstarted: held.unstarted(), whole: whole}, nil
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
	if rebound := firstBinding(operation.Bindings); rebound != "" {
		scope.reopen = rebound
	}
	scope.release = slices.Clone(scope.release)
	for _, member := range folded {
		if member.operation.Verb == reconciliation.Destroy {
			scope.release = joinBindings(scope.release, member.operation.Bindings...)
		}
	}
	return liveState{source: head.ID, scope: scope, states: held, whole: scope.owned}, nil
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

func (s Service) stagedScope(name string, floor reconciliation.Stage, held liveState) ([]string, error) {
	live, states := held.scope.owned, held.states
	scope := s.closure(live, floor)
	if below := belowFloor(live, scope, floor); len(below) != 0 {
		lower := s.widened(live, floor)
		remedy := "take back the whole context with " + contextCommand(name, "destroy --dry-run")
		if reconciliation.ValidFloor(lower) {
			remedy = "take back from a lower stage with " + contextCommand(name, "destroy --dry-run", "--stage", string(lower))
		}
		return nil, destroyStageBelowFloor.Fail(
			"these steps below the "+string(floor)+" stage depend on what a staged destroy from it takes back: "+strings.Join(below, ", "), remedy)
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
		return nil, destroyStageKeepsUnfinished.Fail(
			"a staged destroy from the "+string(floor)+" stage would keep these steps, which are not done: "+strings.Join(unfinished, ", "),
			"finish them with "+contextCommand(name, "apply")+", take them back with a lower stage, or take back the whole context with "+contextCommand(name, string(reconciliation.Destroy)))
	}
	if len(scope) == 0 {
		return scope, nil
	}
	return scope, s.refuseKeptHosts(name, floor, live, held.whole, scope, held.unstarted)
}

func (s Service) widened(live reconciliation.Plan, floor reconciliation.Stage) reconciliation.Stage {
	lower := floor
	for scope := s.closure(live, floor); ; {
		next := lowestStage(live, belowFloor(live, scope, lower))
		if next == "" || !reconciliation.ValidFloor(next) {
			if next != "" {
				lower = next
			}
			return lower
		}
		lower, scope = next, s.closure(live, next)
	}
}

func (s Service) closure(live reconciliation.Plan, floor reconciliation.Stage) []string {
	in := map[string]bool{}
	for _, block := range live.Blocks {
		if block.Stage.AtOrAbove(floor) {
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

func (s Service) refuseKeptHosts(name string, floor reconciliation.Stage, live, whole reconciliation.Plan, scope, unstarted []string) error {
	managed, deleted := map[string]bool{}, map[string]bool{}
	for _, block := range live.Blocks {
		if block.Kind != string(api.Machine) {
			continue
		}
		managed[block.Object] = true
		capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
		if deletion, deletes := capability.(MachineDeletion); ok && deletes && deletion.DeletesMachine(block) && slices.Contains(scope, block.ID) {
			deleted[block.Object] = true
		}
	}
	kept := func(machine string) bool { return managed[machine] && !deleted[machine] }
	for _, id := range slices.Concat(scope, unstarted) {
		block, _ := whole.Block(id)
		machines := s.boundMachines(whole, block)
		slices.Sort(machines)
		machines = slices.DeleteFunc(slices.Compact(machines), func(machine string) bool { return !kept(machine) })
		if len(machines) == 0 {
			continue
		}
		reason := "these Machines stay below the " + string(floor) + " stage, and no step is yet taken back on a Machine it keeps (B1860, B1861)"
		remedy := "take back from the machines stage with " + contextCommand(name, "destroy --dry-run", "--stage", string(reconciliation.StageMachines)) +
			", or take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
		if slices.ContainsFunc(machines, func(machine string) bool { return physicalInScope(live, scope, machine) }) {
			reason = "a physical Machine is always kept, and no staged destroy takes back an installation on it yet (B1864)"
			remedy = "take back the whole context with " + contextCommand(name, string(reconciliation.Destroy))
		}
		return destroyStageKeepsHost.Fail("the step "+id+" runs on Machine/"+strings.Join(machines, ", Machine/")+": "+reason, remedy)
	}
	return nil
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

func stageChanged(name string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, floor reconciliation.Stage) error {
	message := "the unfinished staged destroy " + operation.ID + " froze the " + string(operation.Stage.Floor) + " stage"
	if floor == "" {
		message += ", and a destroy without --stage would also take back the steps it keeps"
	} else {
		message += ", not the " + string(floor) + " stage"
	}
	return destroyStageChanged.Fail(message,
		"finish it with "+continuationCommand(name, operation, frozen, states)+"; once it completes, a lower --stage or a destroy without --stage takes back more")
}

func refuseTrimmed(name string, operation operationstore.Operation, preparation bool) error {
	took := "the staged destroy " + operation.ID + " took back the " + string(operation.Stage.Floor) + " stage and everything built on it"
	remedy := "take back what the context keeps with " + contextCommand(name, string(reconciliation.Destroy)) + ", then apply the changed input"
	if preparation {
		return prepareRefused.Fail(took+", and only an apply restores it", "restore it with "+contextCommand(name, string(reconciliation.Apply))+" first")
	}
	return applyContextTrimmed.Fail(took+", and the input changed since the steps it kept froze theirs, which a restoring apply does not take yet (B1859)", remedy)
}

func (s Service) withFloor(floor string, relaxations map[string]string) (Service, error) {
	if floor == "" {
		return s, nil
	}
	if !reconciliation.ValidFloor(reconciliation.Stage(floor)) {
		return s, engineInputInvalid.Fail("--stage "+floor+" is no stage a destroy takes back from", "name one of substrates, machines, clusters or add-ons, or destroy without --stage")
	}
	for machine, method := range relaxations {
		if method == Abandon {
			return s, engineInputInvalid.Fail("--authorize abandon="+machine+" leaves steps in place, which a staged destroy never does", "repeat without abandon=, or destroy without --stage")
		}
	}
	s.floor = reconciliation.Stage(floor)
	return s, nil
}

func keptSteps(ctx context.Context, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) ([]string, error) {
	folded, err := removalLineage(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return nil, err
	}
	live, _ := folded.live()
	kept := folded.kept()
	return slices.DeleteFunc(live, func(id string) bool { return !kept[id] }), nil
}

func (t operationPlan) floor() reconciliation.Stage {
	switch {
	case t.verb != reconciliation.Destroy:
		return ""
	case t.stage != nil:
		return t.stage.Floor
	case t.requested != "":
		return t.requested
	case staged(t.operation) && !t.fresh:
		return t.operation.Stage.Floor
	}
	return ""
}

func confirmedAction(decided operationPlan) string {
	floor := decided.floor()
	if decided.verb != reconciliation.Destroy || floor == "" {
		return string(decided.verb)
	}
	return "destroy --stage " + string(floor) + ", taking back " + strconv.Itoa(len(decided.plan.Blocks)) + " steps and keeping " + strconv.Itoa(len(decided.kept)) + ","
}

func (t operationPlan) scope() *StagedScope {
	floor := t.floor()
	if t.verb != reconciliation.Destroy || floor == "" {
		return nil
	}
	scope := &StagedScope{Floor: string(floor), Removed: []string{}, Kept: append([]string{}, t.kept...), Residue: []string{}, KeptPowerTargets: []string{}}
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

type StagedScope struct {
	Floor            string
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
		if trimmed(operation) && d.preparation != nil {
			return operationPlan{}, true, refuseTrimmed(view.Identity().Name, operation, true)
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
