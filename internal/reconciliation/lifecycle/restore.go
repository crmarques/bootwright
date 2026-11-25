package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func restoredLineage(ctx context.Context, store recordReader, removal operationstore.Operation) (lineage, error) {
	held, err := statedLineage(ctx, store, removal.Source)
	if err != nil {
		return nil, err
	}
	restored, err := restoredMember(ctx, store, removal.ID, removal.Source)
	if err != nil {
		return nil, err
	}
	return append(held, restored), nil
}

func (d decider) restoring(ctx context.Context, view View, store decisionRecords, removal operationstore.Operation, selection reconciliation.Selection) (operationPlan, error) {
	name := view.Identity().Name
	head, err := store.ReadOperation(ctx, removal.Source)
	if err != nil {
		return operationPlan{}, err
	}
	if head.Verb != reconciliation.Apply {
		return operationPlan{}, notAnApply()
	}
	if inputDrift(view, head) != "" {
		return operationPlan{}, refuseTrimmed(name, removal, false)
	}
	chain, err := restoredLineage(ctx, store, removal)
	if err != nil {
		return operationPlan{}, err
	}
	live, err := chain.ownedPlan()
	if err != nil {
		return operationPlan{}, unremovable(head, err)
	}
	state, err := d.sources.compile(ctx, view)
	if err != nil {
		return operationPlan{}, err
	}
	if err := d.planner.refuseUnsupported(state, nil); err != nil {
		return operationPlan{}, err
	}
	fresh, binding, err := d.planner.planFrom(ctx, view, state, nil)
	if err != nil {
		return operationPlan{}, err
	}
	if err := d.planner.refuseKeptChanged(name, removal, live, fresh); err != nil {
		return operationPlan{}, err
	}
	added, err := reconciliation.Restoring(fresh, live)
	if err != nil {
		return operationPlan{}, unremovable(head, err)
	}
	merged, err := chain.merged(added)
	if err != nil {
		return operationPlan{}, unremovable(head, err)
	}
	if err := d.proofs.restoreProved(ctx, view, chain, live, added); err != nil {
		return operationPlan{}, err
	}
	if selection, err = bindSelection(name, added, selection); err != nil {
		return operationPlan{}, err
	}
	if err := refuseStageBoundary(name, added, nil, selection); err != nil {
		return operationPlan{}, err
	}
	reopen := firstBinding(removal.Bindings)
	if reopen == "" {
		reopen = chain.applies().reopened()
	}
	return operationPlan{
		fresh: true, verb: reconciliation.Apply, plan: added, selection: selection, binding: binding,
		predecessor: head.ID, restores: removal.ID, lineagePlan: merged, reopen: reopen,
		basis: basis{revision: view.Identity().Revision, input: inputDigest(view)},
	}, nil
}

func (s Service) refuseKeptChanged(name string, removal operationstore.Operation, live, fresh reconciliation.Plan) error {
	for _, change := range reconciliation.Compare(live, fresh) {
		if change.Change != reconciliation.Removed && change.Change != reconciliation.Changed {
			continue
		}
		why := "would freeze it differently"
		if change.Change == reconciliation.Removed {
			why = "plans it no more"
		}
		block, _ := live.Block(change.Block)
		remedy := "take back the whole context with " + contextCommand(name, "destroy --dry-run") + ", then apply again"
		if floor := s.widened(live, block.Stage); reconciliation.ValidFloor(block.Stage) && reconciliation.ValidFloor(floor) {
			remedy = "take it back with " + contextCommand(name, "destroy --dry-run", "--stage", string(floor)) + ", then apply again"
		}
		return applyKeptBlockChanged.Fail("the staged destroy "+removal.ID+" kept the step "+change.Block+", and this executable "+why+" from the input the context keeps", remedy)
	}
	return nil
}

func (s Service) restoreProved(ctx context.Context, view View, chain lineage, live, added reconciliation.Plan) error {
	if err := s.proveLineageContracts(chain); err != nil {
		return err
	}
	contracts, err := s.executionContracts(added)
	if err != nil {
		return err
	}
	for _, member := range chain.applies() {
		for implementation, contract := range member.operation.Contracts {
			if current, found := contracts[implementation]; found && current != contract {
				return engineOtherBuild.Fail("this executable would freeze another execution contract for "+implementation+" than the apply "+member.operation.ID+" froze", removeWith(member.operation.Executable))
			}
		}
	}
	definitions := append(planDefinitions(live), planDefinitions(added)...)
	read := s.producedInContext(view.Identity().Name)
	for _, block := range added.Blocks {
		references, err := s.producedReferences(block)
		if err != nil {
			return err
		}
		if len(references) == 0 {
			continue
		}
		lend, release, err := s.producedLender(block, definitions, nil, read)
		if err != nil {
			return err
		}
		for _, reference := range references {
			producer, err := s.producedProducer(reference, definitions)
			if _, restored := added.Block(producer); err == nil && restored {
				continue
			}
			material, err := lend(ctx, reference)
			material.Clear()
			if err != nil {
				release()
				return err
			}
		}
		release()
	}
	return nil
}

func (s Service) proveRestoredBinding(name string, decided operationPlan, material map[string]secrets.Material) error {
	if decided.restores == "" {
		return nil
	}
	for _, block := range decided.plan.Blocks {
		capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
		referencer, names := capability.(SecretReferencer)
		if !found || !names {
			continue
		}
		references, err := referencer.SecretReferences(block)
		if err != nil {
			return err
		}
		for _, reference := range references {
			if _, pinned := material[reference]; !pinned {
				return applyRestoreUnbound.Fail(
					"the Secret binding "+decided.reopen+" this restore reopens pins no version of Secret "+reference+", which the step "+block.ID+" names",
					"take back what the context keeps with "+contextCommand(name, string(reconciliation.Destroy))+", then apply again")
			}
		}
	}
	return nil
}
