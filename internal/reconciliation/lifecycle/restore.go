package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate"
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

type restorePlan struct {
	removal   string
	head      operationstore.Operation
	chain     lineage
	live      reconciliation.Plan
	fresh     reconciliation.Plan
	binding   capabilityBinding
	unplanned error
}

func (d decider) planRestore(ctx context.Context, view View, store recordReader, removal operationstore.Operation) (restorePlan, error) {
	if d.restore != nil && d.restore.removal == removal.ID {
		return *d.restore, nil
	}
	head, err := store.ReadOperation(ctx, removal.Source)
	if err != nil {
		return restorePlan{}, err
	}
	if head.Verb != reconciliation.Apply {
		return restorePlan{}, notAnApply()
	}
	chain, err := restoredLineage(ctx, store, removal)
	if err != nil {
		return restorePlan{}, err
	}
	live, err := chain.ownedPlan()
	if err != nil {
		return restorePlan{}, unremovable(head, err)
	}
	planned := restorePlan{removal: removal.ID, head: head, chain: chain, live: live}
	state, err := d.sources.compile(ctx, view)
	if err == nil {
		err = d.planner.refuseUnsupported(state, nil)
	}
	if err == nil {
		planned.fresh, planned.binding, err = d.planner.planFrom(ctx, view, state, nil)
	}
	planned.unplanned = err
	return planned, nil
}

func (d decider) restorable(ctx context.Context, view View, store recordReader, removal operationstore.Operation) (bool, error) {
	planned, err := d.planRestore(ctx, view, store, removal)
	if err != nil {
		return false, err
	}
	_, refused := d.admitRestore(view.Identity().Name, removal, planned)
	return refused == nil, nil
}

func (d decider) admitRestore(name string, removal operationstore.Operation, planned restorePlan) (KeptChange, error) {
	if planned.unplanned != nil {
		return KeptChange{Removal: removal.ID, Unchecked: firstMessage(planned.unplanned)}, planned.unplanned
	}
	if changed, remedy := d.planner.keptChanges(name, planned.live, planned.fresh); len(changed) != 0 {
		why := "would freeze it differently"
		if changed[0].Change == reconciliation.Removed {
			why = "plans it no more"
		}
		kept := KeptChange{Removal: removal.ID, Remedy: remedy}
		for _, change := range changed {
			kept.Steps = append(kept.Steps, change.Block)
		}
		return kept, applyKeptBlockChanged.Fail("the staged destroy "+removal.ID+" kept the step "+changed[0].Block+", and this executable "+why+" from the selected input", remedy)
	}
	restorable := append(slices.Clone(removal.Stage.TakenBack), planned.chain.unstarted()...)
	var added, unknown []string
	for _, block := range planned.fresh.Blocks {
		if _, held := planned.live.Block(block.ID); held {
			continue
		}
		added = append(added, block.ID)
		if !slices.Contains(restorable, block.ID) {
			unknown = append(unknown, block.ID)
		}
	}
	switch {
	case len(unknown) != 0:
		remedy := "restore without " + strings.Join(unknown, ", ") + " by importing an input that does not declare it and running " + contextCommand(name, string(reconciliation.Apply)) +
			"; then an add-on instance is added by the additive apply, and anything else needs the whole context taken back with " + contextCommand(name, "destroy --dry-run") + ", then apply again"
		return KeptChange{Removal: removal.ID, Added: unknown, Remedy: remedy}, applyRestoreAddsNew.Fail(
			"the selected input declares the step "+strings.Join(unknown, ", ")+", which no staged destroy of this context took back, and a restoring apply re-creates only what one took back", remedy)
	case len(added) == 0:
		remedy := "take back what the context keeps with " + contextCommand(name, "destroy --dry-run") + " and " + contextCommand(name, string(reconciliation.Destroy)) +
			", or import an input that declares again what it took back (" + strings.Join(removal.Stage.TakenBack, ", ") + ") and apply"
		return KeptChange{Removal: removal.ID, Empty: true, Remedy: remedy}, applyRestoreEmpty.Fail(
			"the selected input declares nothing the staged destroy "+removal.ID+" took back, so there is nothing to restore", remedy)
	}
	return KeptChange{}, nil
}

func (d decider) restoring(ctx context.Context, view View, store decisionRecords, removal operationstore.Operation, selection reconciliation.Selection) (operationPlan, error) {
	name := view.Identity().Name
	planned, err := d.planRestore(ctx, view, store, removal)
	if err != nil {
		return operationPlan{}, err
	}
	if _, err := d.admitRestore(name, removal, planned); err != nil {
		return operationPlan{}, err
	}
	head, chain, live := planned.head, planned.chain, planned.live
	added, err := reconciliation.Restoring(planned.fresh, live)
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
		fresh: true, verb: reconciliation.Apply, plan: added, selection: selection, binding: planned.binding,
		predecessor: head.ID, restores: removal.ID, lineagePlan: merged, reopen: reopen,
		basis: basis{revision: view.Identity().Revision, input: inputDigest(view)},
	}, nil
}

func (s Service) keptChanges(name string, live, fresh reconciliation.Plan) ([]reconciliation.BlockChange, string) {
	var changed []reconciliation.BlockChange
	var floor reconciliation.Stage
	whole := false
	for _, change := range reconciliation.Compare(live, fresh) {
		if change.Change != reconciliation.Removed && change.Change != reconciliation.Changed {
			continue
		}
		changed = append(changed, change)
		block, _ := live.Block(change.Block)
		lower := s.widened(live, block.Stage)
		switch {
		case !reconciliation.ValidFloor(block.Stage) || !reconciliation.ValidFloor(lower):
			whole = true
		case floor == "" || floor.AtOrAbove(lower):
			floor = lower
		}
	}
	if whole || floor == "" {
		return changed, "take back the whole context with " + contextCommand(name, "destroy --dry-run") + ", then apply again"
	}
	return changed, "take it back with " + contextCommand(name, "destroy --dry-run", "--stage", string(floor)) + ", then apply again"
}

type KeptChange struct {
	Removal   string
	Steps     []string
	Added     []string
	Empty     bool
	Remedy    string
	Unchecked string
}

func (s Service) KeptChanges(ctx context.Context, name string, candidate desiredstate.Sources) (KeptChange, error) {
	if err := s.available(ctx); err != nil {
		return KeptChange{}, err
	}
	var kept KeptChange
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		removal, err := store.ReadOperation(ctx, index.Current)
		if err != nil || !trimmed(removal) {
			return err
		}
		decision := s.decider(nil)
		planned, err := decision.planRestore(ctx, candidateView{View: view, inputs: candidate}, store, removal)
		if err != nil {
			kept = KeptChange{Removal: removal.ID, Unchecked: firstMessage(err)}
			return nil
		}
		kept, _ = decision.admitRestore(name, removal, planned)
		return nil
	})
	return kept, err
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
