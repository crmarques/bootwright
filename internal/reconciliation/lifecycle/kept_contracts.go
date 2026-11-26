package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type keptContract struct {
	implementation, fingerprint, subject string
}

func (s Service) keptContracts(ctx context.Context, store recordReader, operation operationstore.Operation, block reconciliation.Block) ([]keptContract, error) {
	var contracts []keptContract
	if port, carries := s.capabilityOf(block).(KeptHostsContract); carries && len(frozenKeptHosts(block)) != 0 {
		implementation, digest := port.KeptHostsContract()
		contract, err := s.keptContractOf(block, "kept-hosts", implementation, digest, "kept-host contract of block "+block.ID)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	if len(frozenBlanked(block)) == 0 {
		return contracts, nil
	}
	targets, err := s.resetTargets(ctx, store, operation, block)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		implementation, digest := target.port.RootResetContract()
		contract, err := s.keptContractOf(target.original, "root-reset", implementation, digest, "root reset contract of Machine/"+target.machine)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

func (s Service) keptContractOf(owner reconciliation.Block, effect, implementation, digest, subject string) (keptContract, error) {
	contract := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: owner.ID + "/" + effect, Kind: owner.Kind, Object: owner.Object,
		Implementation: implementation, ContentDigest: digest}}
	contracts, err := s.executionContracts(reconciliation.Plan{Blocks: []reconciliation.Block{contract}})
	return keptContract{implementation: implementation, fingerprint: contracts[implementation], subject: subject}, err
}

func (s Service) recordKeptContracts(ctx context.Context, store recordReader, plan reconciliation.Plan, source string, stage *operationstore.Stage, contracts map[string]string) error {
	operation := operationstore.Operation{Source: source, Stage: stage}
	for _, block := range plan.Blocks {
		kept, err := s.keptContracts(ctx, store, operation, block)
		if err != nil {
			return err
		}
		for _, contract := range kept {
			if held, found := contracts[contract.implementation]; found && held != contract.fingerprint {
				return contractFailure("block %s names a conflicting execution contract", block.ID)
			}
			contracts[contract.implementation] = contract.fingerprint
		}
	}
	return nil
}

func (s Service) proveKeptContract(ctx context.Context, store recordReader, operation operationstore.Operation, block reconciliation.Block) error {
	kept, err := s.keptContracts(ctx, store, operation, block)
	if err != nil {
		return err
	}
	for _, contract := range kept {
		if operation.Contracts[contract.implementation] != contract.fingerprint {
			return contractFailure("this executable does not support the recorded %s", contract.subject)
		}
	}
	return nil
}

func (s Service) proveAndReread(ctx context.Context, run invocation, capability Capability, execution Execution, reread bool) (Result, bool, error) {
	if err := s.proveKeptContract(ctx, run.store, run.operation, execution.Block); err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, true, err
	}
	if !reread {
		return Result{}, false, nil
	}
	return rereadBeforeRemoval(ctx, s.removalObserver(run, capability), execution)
}

func (s Service) proveKeptContracts(ctx context.Context, store recordReader, plan reconciliation.Plan, operation operationstore.Operation) error {
	for _, block := range plan.Blocks {
		if err := s.proveKeptContract(ctx, store, operation, block); err != nil {
			return err
		}
	}
	return nil
}
