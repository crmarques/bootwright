package lifecycle

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

type removalMachineGroup struct {
	machines []reconciliation.Block
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

func (s Service) recordedMachinePower(ctx context.Context, run invocation, execution Execution, block reconciliation.Block) (BlockEvidence, Quiescence, error) {
	chain, err := appliedLineage(ctx, run.store, run.operation)
	if err != nil {
		return BlockEvidence{}, Quiescence{}, err
	}
	member, held := chain.ownerOf(block.ID)
	original, found := member.plan.Block(block.ID)
	if !held || !found || original.Implementation != block.Implementation || original.RequestDigest != block.RequestDigest || original.Object != block.Object || original.Kind != block.Kind {
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
	state, err := reader.StartPower(ctx, Probe{ApplyOperation: owner, Context: execution.Context, Block: original, ApplyProved: deps, PreviousEvidence: proof.Evidence, Launch: execution.Launch, Material: execution.Material})
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
		if err := s.options.DriveMachine(ctx, execution, proof, false); err != nil {
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

func (s Service) removalStart(ctx context.Context, tx Transaction, store OperationStore, located approved, decided operationPlan, material map[string]secrets.Material) ([]*removalMachineGroup, error) {
	if decided.verb != reconciliation.Destroy {
		return nil, nil
	}
	if err := survivorRefusal(decided.plan.Blocks, decided.phases.survivors); err != nil {
		return nil, err
	}
	skip := maps.Clone(decided.phases.machines)
	maps.Copy(skip, decided.phases.preStopMachines)
	var groups []*removalMachineGroup
	if decided.basis.record.Verb == reconciliation.Destroy {
		if group := s.deferMachineStops(decided.plan, skip); group != nil {
			groups = append(groups, group)
		}
	}
	if !decided.fresh {
		return groups, nil
	}
	if err := s.proveRemovable(ctx, tx, store, located, decided, material); err != nil {
		return nil, err
	}
	chain, err := sourceLineage(ctx, store, decided.source)
	if err != nil {
		return nil, err
	}
	err = s.proveQuiescent(ctx, store, tx.Identity().Name, located, decided.plan, decided.phases, material, chain, skip)
	return groups, err
}
