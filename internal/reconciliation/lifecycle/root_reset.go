package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func frozenBlanked(block reconciliation.Block) []string {
	if block.Inverse == nil {
		return nil
	}
	return block.Inverse.Blanks
}

type resetTarget struct {
	machine  string
	member   lineageMember
	original reconciliation.Block
	port     RootDiskReset
}

func (s Service) removeBlanking(ctx context.Context, run invocation, capability Capability, execution Execution) (Result, error) {
	if len(frozenBlanked(execution.Block)) == 0 {
		return capability.Destroy(ctx, execution)
	}
	targets, err := s.resetTargets(ctx, run.store, run.operation, execution.Block, true)
	if err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := capability.Destroy(ctx, execution)
	if err != nil || !result.Outcome.Settled() {
		return result, err
	}
	changed, effect, err := s.rootDisks(ctx, run, execution, targets, true)
	switch {
	case err != nil:
		result.Outcome = AttemptOutcome(err)
		return result, err
	case effect != reconciliation.EffectCompleted:
		result.Outcome = reconciliation.OutcomeUnknown
		return result, destroyPostconditionUnknown.Fail("a kept Machine's root disk was not proved blank after its reset", "")
	case changed:
		result.Outcome = reconciliation.OutcomeChanged
	}
	return result, nil
}

func (s Service) removalObserver(run invocation, capability Capability) func(context.Context, Execution) (Observation, error) {
	return func(ctx context.Context, execution Execution) (Observation, error) {
		return s.observeBlanking(ctx, run, capability, execution)
	}
}

func (s Service) observeBlanking(ctx context.Context, run invocation, capability Capability, execution Execution) (Observation, error) {
	if run.operation.Verb != reconciliation.Destroy || len(frozenBlanked(execution.Block)) == 0 {
		return capability.ObserveRemoval(ctx, execution)
	}
	targets, err := s.resetTargets(ctx, run.store, run.operation, execution.Block, true)
	if err != nil {
		return Observation{Effect: reconciliation.EffectUnknown}, err
	}
	observed, err := capability.ObserveRemoval(ctx, execution)
	if err != nil || observed.Effect != reconciliation.EffectCompleted {
		return observed, err
	}
	_, effect, err := s.rootDisks(ctx, run, execution, targets, false)
	switch {
	case err != nil || effect == reconciliation.EffectUnknown:
		observed.Effect = reconciliation.EffectUnknown
	case effect != reconciliation.EffectCompleted:
		observed.Effect = reconciliation.EffectPartial
	}
	return observed, err
}

func (s Service) rootDisks(ctx context.Context, run invocation, execution Execution, targets []resetTarget, reset bool) (bool, reconciliation.EffectState, error) {
	combined, changed := reconciliation.EffectCompleted, false
	for _, target := range targets {
		request, err := s.rootReset(ctx, run, execution, target)
		if err != nil {
			return changed, reconciliation.EffectUnknown, err
		}
		effect, outcome := reconciliation.EffectUnknown, reconciliation.OutcomeUnchanged
		if reset {
			if execution.Log != nil {
				if err := execution.Log(ctx, operationstore.LogRecord{Event: "root-reset", Block: execution.Block.ID, Detail: "Machine/" + target.machine}); err != nil {
					return changed, reconciliation.EffectUnknown, err
				}
			}
			outcome, effect, err = target.port.ResetRootDisk(ctx, request)
		} else {
			effect, err = target.port.ObserveRootDisk(ctx, request)
		}
		changed = changed || outcome == reconciliation.OutcomeChanged
		switch {
		case err != nil:
			return changed, reconciliation.EffectUnknown, err
		case effect == reconciliation.EffectUnknown || reset && effect != reconciliation.EffectCompleted:
			return changed, effect, nil
		case effect != reconciliation.EffectCompleted:
			combined = effect
		}
	}
	return changed, combined, nil
}

func (s Service) resetTargets(ctx context.Context, store recordReader, operation operationstore.Operation, block reconciliation.Block, proved bool) ([]resetTarget, error) {
	held, err := sourceLineage(ctx, store, operation.Source)
	if err != nil {
		return nil, err
	}
	owned := held.owners()
	var targets []resetTarget
	for _, machine := range frozenBlanked(block) {
		target, found := s.resetTargetOf(operation, owned, machine)
		if !found {
			return nil, engineRecordsDamaged.Fail("the removal blanks the root disk of Machine/"+machine+", which no kept step of its lineage can reset", "restore the context store")
		}
		if refusal := target.port.RootResetRefusal(target.original); refusal != "" {
			return nil, destroyInverseMoved.Fail("the removal plan froze that the step "+block.ID+" blanks the root disk of Machine/"+machine+", and now Machine/"+machine+" "+refusal,
				"continue or replace the removal "+operation.ID+" with "+registeredWith(operation.Executable))
		}
		if err := s.proveContracts(reconciliation.Plan{Blocks: []reconciliation.Block{target.original}}, target.member.operation.Contracts); err != nil {
			return nil, err
		}
		if proved {
			implementation, fingerprint, err := s.resetContract(target)
			if err != nil {
				return nil, err
			}
			if operation.Contracts[implementation] != fingerprint {
				return nil, contractFailure("this executable does not support the recorded root reset contract of Machine/%s", machine)
			}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (s Service) resetTargetOf(operation operationstore.Operation, owned owners, machine string) (resetTarget, bool) {
	if operation.Stage == nil {
		return resetTarget{}, false
	}
	for _, kept := range operation.Stage.KeptPower {
		if kept.Machine != machine {
			continue
		}
		member, original, found := owned.of(kept.Block)
		port, resets := s.capabilityOf(original).(RootDiskReset)
		if found && resets && original.Object == machine {
			return resetTarget{machine: machine, member: member, original: original, port: port}, true
		}
	}
	return resetTarget{}, false
}

func (s Service) resetContract(target resetTarget) (string, string, error) {
	implementation, digest := target.port.RootResetContract()
	contract := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: target.original.ID + "/root-reset", Kind: target.original.Kind, Object: target.original.Object,
		Implementation: implementation, ContentDigest: digest}}
	contracts, err := s.executionContracts(reconciliation.Plan{Blocks: []reconciliation.Block{contract}})
	return implementation, contracts[implementation], err
}

func (s Service) recordResetContracts(ctx context.Context, store recordReader, plan reconciliation.Plan, source string, stage *operationstore.Stage, contracts map[string]string) error {
	operation := operationstore.Operation{Source: source, Stage: stage}
	for _, block := range plan.Blocks {
		if len(frozenBlanked(block)) == 0 {
			continue
		}
		targets, err := s.resetTargets(ctx, store, operation, block, false)
		if err != nil {
			return err
		}
		for _, target := range targets {
			implementation, fingerprint, err := s.resetContract(target)
			if err != nil {
				return err
			}
			if held, found := contracts[implementation]; found && held != fingerprint {
				return contractFailure("block %s names a conflicting execution contract", block.ID)
			}
			contracts[implementation] = fingerprint
		}
	}
	return nil
}

func (s Service) proveResetContracts(ctx context.Context, store recordReader, plan reconciliation.Plan, operation operationstore.Operation) error {
	for _, block := range plan.Blocks {
		if len(frozenBlanked(block)) == 0 {
			continue
		}
		if _, err := s.resetTargets(ctx, store, operation, block, true); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) rootReset(ctx context.Context, run invocation, execution Execution, target resetTarget) (RootReset, error) {
	original := target.original
	references, err := target.port.RootResetSecrets(original)
	if err != nil {
		return RootReset{}, err
	}
	presented, lent := run.material.of(original.ID), map[string]secrets.Material{}
	for _, reference := range references {
		value, found := presented[reference]
		if !found {
			return RootReset{}, startPowerRefused.Fail("the staged destroy's binding holds no access for the kept Machine/"+original.Object, "restore its frozen credentials")
		}
		lent[reference] = value
	}
	owner := target.member.operation.ID
	deps, err := s.applyDependencies(ctx, run.store, owner, original.ID)
	if err != nil {
		return RootReset{}, err
	}
	previous, err := previousBlockEvidence(ctx, run.store, owner, original.ID)
	if err != nil {
		return RootReset{}, err
	}
	machine := Execution{ApplyOperation: owner, Context: execution.Context, Block: original, ApplyProved: deps, PreviousEvidence: previous, Launch: execution.Launch, Material: lent,
		Attempt: execution.Attempt, Resolution: execution.Resolution, Log: execution.Log}
	return RootReset{Execution: machine, Removal: run.operation.ID, Step: execution.Block.ID}, nil
}
