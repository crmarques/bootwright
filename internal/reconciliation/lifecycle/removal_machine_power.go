package lifecycle

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type removalMachineGroup struct {
	machines []reconciliation.Block
	before   map[string]bool
	after    []string
	once     sync.Once
	err      error
}

func (s Service) deferMachineStops(plan reconciliation.Plan, skip map[string]bool) *removalMachineGroup {
	bindings := s.capabilities.Bindings()
	var machines []reconciliation.Block
	for _, block := range plan.Blocks {
		if !skip[block.ID] && managedPowerTarget(bindings, block) {
			machines = append(machines, block)
			skip[block.ID] = true
		}
	}
	if len(machines) == 0 {
		return nil
	}
	return &removalMachineGroup{machines: machines}
}

func (c *scheduler) deferMachineGroup(skip map[string]bool) {
	if c.observeOnly {
		return
	}
	if group := c.service.deferMachineStops(c.plan, skip); group != nil {
		c.stopGroups = append(slices.Clone(c.stopGroups), group)
	}
}

func (s Service) retirementStopGroups(decided operationPlan, skip map[string]bool) ([]*removalMachineGroup, error) {
	waitFor := map[string][]string{}
	for _, block := range decided.plan.Blocks {
		if !decided.phases.retired[block.ID] || decided.basis.blocks[block.ID] == reconciliation.BlockDone {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		online, ok := capability.(OnlineRetirement)
		if !ok {
			continue
		}
		hosts, err := online.OnlineRetirementHosts(block)
		if err != nil {
			return nil, err
		}
		for _, host := range hosts {
			waitFor[host] = append(waitFor[host], block.ID)
		}
	}
	bindings := s.capabilities.Bindings()
	var groups []*removalMachineGroup
	byPrerequisites := map[string]*removalMachineGroup{}
	for _, block := range decided.plan.Blocks {
		after := waitFor[block.Object]
		if skip[block.ID] || len(after) == 0 || !managedPowerTarget(bindings, block) {
			continue
		}
		key := strings.Join(after, "\x00")
		group := byPrerequisites[key]
		if group == nil {
			group = &removalMachineGroup{before: dependentsOfAll(decided.plan, after), after: after}
			byPrerequisites[key] = group
			groups = append(groups, group)
		}
		if !group.before[block.ID] {
			return nil, engineRecordsDamaged.Fail("Machine/"+block.Object+" does not follow its online retirement", "restore the context's original removal records")
		}
		group.machines = append(group.machines, block)
		skip[block.ID] = true
	}
	return groups, nil
}

func (g *removalMachineGroup) ready(ctx context.Context, run invocation, block string) (bool, error) {
	if g.before != nil && !g.before[block] {
		return false, nil
	}
	for _, id := range g.after {
		record, err := run.store.Block(ctx, run.operation.ID, id)
		if err != nil || record.State != reconciliation.BlockDone {
			return false, err
		}
	}
	return true, nil
}

func dependentsOfAll(plan reconciliation.Plan, prerequisites []string) map[string]bool {
	counts := map[string]int{}
	for _, prerequisite := range prerequisites {
		reached := map[string]bool{prerequisite: true}
		for _, block := range plan.Blocks {
			if slices.ContainsFunc(block.Dependencies, func(id string) bool { return reached[id] }) {
				reached[block.ID] = true
				counts[block.ID]++
			}
		}
	}
	result := map[string]bool{}
	for id, count := range counts {
		if count == len(prerequisites) {
			result[id] = true
		}
	}
	return result
}

func (s Service) recordedMachinePower(ctx context.Context, run invocation, execution Execution, block reconciliation.Block) (BlockEvidence, Quiescence, error) {
	chain, err := appliedLineage(ctx, run.store, run.operation)
	if err != nil {
		return BlockEvidence{}, Quiescence{}, err
	}
	member, original, held := chain.owners().of(block.ID)
	if !held || original.Implementation != block.Implementation || original.RequestDigest != block.RequestDigest || original.Object != block.Object || original.Kind != block.Kind {
		return BlockEvidence{}, Quiescence{}, engineRecordsDamaged.Fail("the original apply does not identify Machine/"+block.Object, "restore its original apply records")
	}
	capability, _ := s.capabilities.Resolve(original.Kind, original.Implementation)
	reader, ok := capability.(MachineStartPower)
	if !ok {
		return BlockEvidence{}, Quiescence{}, engineOtherBuild.Fail("this build cannot read native power for Machine/"+block.Object, "use a build with its original substrate implementation")
	}
	owner := member.operation.ID
	proof, err := blockEvidence(ctx, run.store, owner, original, reconciliation.Apply)
	if err != nil {
		return BlockEvidence{}, Quiescence{}, err
	}
	proof.Block = original
	deps, err := s.applyDependencies(ctx, run.store, owner, original.ID)
	if err != nil {
		return BlockEvidence{}, Quiescence{}, err
	}
	state, err := reader.StartPower(ctx, Probe{ApplyOperation: owner, Context: execution.Context, Block: original, ApplyProved: deps, PreviousEvidence: proof.Evidence, Launch: execution.Launch, Material: run.material.of(original.ID)})
	return proof, state, err
}

func (s Service) stopRemovalGroup(ctx context.Context, run invocation, execution Execution, group *removalMachineGroup) error {
	var live []BlockEvidence
	var remaining []reconciliation.Block
	for _, block := range group.machines {
		record, err := run.store.Block(ctx, run.operation.ID, block.ID)
		if err != nil {
			return err
		}
		if record.State == reconciliation.BlockDone {
			continue
		}
		remaining = append(remaining, block)
		proof, state, err := s.recordedMachinePower(ctx, run, execution, block)
		if err != nil {
			return err
		}
		if state.Settled() {
			continue
		}
		if state.State != Live || proof.State != reconciliation.BlockDone || len(proof.Evidence) == 0 || s.options.DriveMachine == nil {
			return destroyInUse.Fail("cannot prove a safe stop for Machine/"+block.Object+" ("+state.Reason+")", "restore its original identity and native power access and repeat the destroy")
		}
		live = append(live, proof)
	}
	for _, proof := range live {
		if err := s.options.DriveMachine(ctx, run.machineExecution(execution, proof.Block.ID), proof, false); err != nil {
			return err
		}
	}
	if len(live) == 0 {
		return nil
	}
	for _, block := range remaining {
		_, state, err := s.recordedMachinePower(ctx, run, execution, block)
		if err != nil {
			return err
		}
		if !state.Settled() {
			return destroyInUse.Fail("Machine/"+block.Object+" could not be proved stopped ("+state.Reason+")", "restore native power access and repeat the destroy")
		}
	}
	return nil
}

func (s Service) removalStart(ctx context.Context, tx Transaction, store OperationStore, located approved, decided operationPlan, material stepMaterial) ([]*removalMachineGroup, map[string]error, error) {
	if decided.verb != reconciliation.Destroy {
		return nil, nil, nil
	}
	if err := survivorRefusal(decided.plan.Blocks, decided.phases.survivors); err != nil {
		return nil, nil, err
	}
	if err := s.proveKeptPower(ctx, store, tx.Identity().Name, located, decided, material); err != nil {
		return nil, nil, err
	}
	skip := maps.Clone(decided.phases.machines)
	maps.Copy(skip, decided.phases.preStopMachines)
	abandoned := abandonedBlocks(decided.plan)
	if skip == nil && len(abandoned) != 0 {
		skip = map[string]bool{}
	}
	maps.Copy(skip, abandoned)
	var groups []*removalMachineGroup

	var deferred map[string]error
	if decided.fresh || len(decided.basis.record.DeferredSourceProofs) != 0 {
		var err error
		deferred, err = s.proveRemovable(ctx, tx, store, located, decided, material)
		if err != nil {
			return nil, nil, err
		}
	}
	if decided.basis.record.Verb == reconciliation.Destroy {
		stopExclusions := maps.Clone(skip)
		maps.Copy(stopExclusions, heldRemovalBlocks(decided.plan, deferred))
		retirementStops, err := s.retirementStopGroups(decided, stopExclusions)
		if err != nil {
			return nil, nil, err
		}
		if group := s.deferMachineStops(decided.plan, stopExclusions); group != nil {
			groups = append(groups, group)
		}
		groups = append(groups, retirementStops...)
		for _, group := range groups {
			for _, block := range group.machines {
				skip[block.ID] = true
			}
		}
	}
	if !decided.fresh {
		return groups, deferred, nil
	}
	chain, err := sourceLineage(ctx, store, decided.source)
	if err != nil {
		return nil, nil, err
	}
	err = s.proveQuiescent(ctx, store, tx.Identity().Name, decided.floor(), decided.stage, located, decided.plan, decided.phases, material, chain, skip)
	return groups, deferred, err
}

func heldRemovalBlocks(plan reconciliation.Plan, deferred map[string]error) map[string]bool {
	if len(deferred) == 0 {
		return nil
	}
	held := map[string]bool{}
	for _, block := range plan.Blocks {
		if deferred[block.ID] != nil || slices.ContainsFunc(block.Dependencies, func(id string) bool { return held[id] }) {
			held[block.ID] = true
		}
	}
	return held
}

func (s Service) proveKeptPower(ctx context.Context, store recordReader, name string, located approved, decided operationPlan, material map[string]secrets.Material) error {
	stage, source := decided.stage, decided.source
	if !decided.fresh {
		stage, source = decided.operation.Stage, decided.operation.Source
	}
	if stage == nil || len(stage.KeptPower) == 0 {
		return nil
	}
	held, err := sourceLineage(ctx, store, source)
	if err != nil {
		return err
	}
	owned := held.owners()
	var blocked, stops []string
	err = s.withPython(ctx, located, func(launch prerequisites.PythonLaunch) error {
		for index, target := range stage.KeptPower {
			member, original, found := owned.of(target.Block)
			reader, reads := s.capabilityOf(original).(MachineStartPower)
			if !found || original.Kind != string(api.Machine) || original.Object != target.Machine || !reads {
				return engineRecordsDamaged.Fail("the staged destroy keeps Machine/"+target.Machine+", which no step of its lineage can read the power of", "restore the context store")
			}
			event := ProgressEvent{Phase: CheckPhase, Block: "kept-power-" + target.Block, Position: index + 1, Total: len(stage.KeptPower),
				Description: "prove the kept Machine/" + target.Machine + " is powered off, changing nothing", Status: "running"}
			s.report(ctx, event)
			state, err := s.readKeptPower(ctx, store, name, member.operation.ID, original, reader, launch, material)
			event.Status = "ok"
			if err != nil || !state.Settled() {
				event.Status = "failed"
			}
			s.report(ctx, event)
			if err != nil {
				return err
			}
			if !state.Settled() {
				blocked = append(blocked, "Machine/"+target.Machine+" ("+state.Reason+")")
				if state.Stop != "" && !slices.Contains(stops, state.Stop) {
					stops = append(stops, state.Stop)
				}
			}
		}
		return nil
	})
	if err != nil || len(blocked) == 0 {
		return err
	}
	repeat := contextCommand(name, string(reconciliation.Destroy), invocationFlags(stage.Floor, reconciliation.Selection{}, authorizingUnfixed(decided.plan))...)
	remedy := "power them off, then repeat " + repeat
	if len(stops) != 0 {
		remedy = "stop them with " + strings.Join(stops, ", then ") + ", then repeat " + repeat
	}
	return destroyInUse.Fail("the staged destroy takes back steps on kept Machines that must stay powered off: "+strings.Join(blocked, ", "), remedy)
}

func (s Service) readKeptPower(ctx context.Context, store recordReader, name, owner string, original reconciliation.Block, reader MachineStartPower, launch prerequisites.PythonLaunch, material map[string]secrets.Material) (Quiescence, error) {
	references, err := reader.StartPowerSecrets(original)
	if err != nil {
		return Quiescence{}, err
	}
	lent := map[string]secrets.Material{}
	for _, reference := range references {
		value, found := material[reference]
		if !found {
			return Quiescence{}, startPowerRefused.Fail("the staged destroy's binding holds no access for the kept Machine/"+original.Object, "restore its frozen credentials")
		}
		lent[reference] = value
	}
	deps, err := s.applyDependencies(ctx, store, owner, original.ID)
	if err != nil {
		return Quiescence{}, err
	}
	previous, err := previousBlockEvidence(ctx, store, owner, original.ID)
	if err != nil {
		return Quiescence{}, err
	}
	return reader.StartPower(ctx, Probe{ApplyOperation: owner, Context: name, Block: original, ApplyProved: deps, PreviousEvidence: previous, Launch: launch, Material: lent})
}
