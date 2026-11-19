package lifecycle

import (
	"context"
	"slices"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// A hosted addition reaches its cluster through the original installation's node set.
func (s Service) startClusterNodes(ctx context.Context, capability Capability, execution Execution) error {
	hosted, ok := capability.(HostedConsumer)
	if !ok {
		return nil
	}
	cluster, err := hosted.HostObject(execution.Block)
	if err != nil {
		return err
	}
	for _, proof := range execution.ApplyProved {
		if proof.Kind != cluster.Kind || proof.Object != cluster.Object {
			continue
		}
		resolved, _ := s.capabilities.Resolve(proof.Kind, proof.Implementation)
		owner, ok := resolved.(NodeBound)
		if !ok {
			continue
		}
		nodes, err := owner.NodeMachines(proof.Block)
		if err != nil {
			return err
		}
		for _, name := range slices.Sorted(slices.Values(nodes)) {
			if err := execution.PowerMachine(ctx, name, true); err != nil {
				return err
			}
		}
		return nil
	}
	return engineRecordsDamaged.Fail("the added block lacks its cluster's original installation proof", "restore the context's apply records")
}

func (s Service) machinePower(run invocation, execution Execution) func(context.Context, string, bool) error {
	return func(ctx context.Context, name string, on bool) error {
		bindings := s.capabilities.Bindings()
		for _, proof := range execution.ApplyProved {
			if proof.Object != name || !managedPowerTarget(bindings, proof.Block) {
				continue
			}
			if proof.State != reconciliation.BlockDone || s.options.DriveMachine == nil {
				break
			}
			if run.operation.Verb == reconciliation.Apply && proof.Operation != run.operation.ID {
				if !on || run.clusterStarts == nil {
					return engineRequestDefect.Fail("an additive apply can only start its installed cluster nodes", "")
				}
				start, _ := run.clusterStarts.LoadOrStore(proof.Block.ID, sync.OnceValue(func() error {
					return s.drivePredecessor(ctx, run, execution, proof)
				}))
				return start.(func() error)()
			}
			return s.options.DriveMachine(ctx, execution, proof, on)
		}
		return engineRecordsDamaged.Fail("no proved managed Machine/"+name+" is available for this power transition", "restore its original apply evidence")
	}
}

func (s Service) drivePredecessor(ctx context.Context, run invocation, execution Execution, proof BlockEvidence) error {
	owner, err := run.store.ReadOperation(ctx, proof.Operation)
	if err != nil {
		return err
	}
	binding := firstBinding(owner.Bindings)
	if binding == "" {
		return engineRecordsDamaged.Fail("the Machine's apply holds no binding for its management credential", "restore its original Secret binding")
	}
	values, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: execution.Context, BindingID: binding})
	if err != nil {
		return err
	}
	material := make(map[string]secrets.Material, len(values))
	for _, value := range values {
		material[value.Version.Declaration.Name] = value.Material
	}
	defer clearMaterial(material)
	execution.Material = material
	return s.options.DriveMachine(ctx, execution, proof, true)
}

func (s Service) machineStopped(run invocation, execution Execution) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, name string) (bool, error) {
		for _, dependency := range execution.ApplyProved {
			if dependency.Object != name || !managedPowerTarget(s.capabilities.Bindings(), dependency.Block) {
				continue
			}
			_, state, err := s.recordedMachinePower(ctx, run, execution, dependency.Block)
			return state.Settled(), err
		}
		return false, engineRecordsDamaged.Fail("the original apply lacks the managed Machine/"+name+" for the power check", "restore its apply records")
	}
}
