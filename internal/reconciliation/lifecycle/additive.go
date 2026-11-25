package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type additiveAdmission struct {
	preparedBinding *capabilityBinding
	plan            reconciliation.Plan
	merged          reconciliation.Plan
	secrets         []string
	controller      string
}

const packagesPath = "spec.packages"

func (d decider) addsObjects(before, after api.Catalog, changes []objectChange) (bool, []string) {
	var secrets, others []string
	bindings := false
	for _, change := range changes {
		kind, name, _ := strings.Cut(change.identity, "/")
		switch {
		case change.change == reconciliation.Added && kind == string(api.Secret):
			secrets = append(secrets, name)
		case change.change == reconciliation.Added && kind != string(api.ClusterAddonBinding):
			others = append(others, change.identity)
		case kind != string(api.ClusterAddonBinding):
			return false, nil
		case change.change == reconciliation.Added:
			bindings = true
		case change.change == reconciliation.Changed:
			previous, found := before.Find(api.ClusterAddonBinding, name)
			if !found || !addsPackageMembers(previous, change.fields) {
				return false, nil
			}
			bindings = true
		default:
			return false, nil
		}
	}
	if len(others) == 0 {
		return bindings, secrets
	}
	var admitted []string
	for _, bound := range d.capabilities.Bindings() {
		capability, found := d.capabilities.Resolve(bound.Kind, bound.Implementation)
		if objects, admits := capability.(AddedObjectsAdmitter); found && admits {
			admitted = append(admitted, objects.AdmitsAddedObjects(after, others)...)
		}
	}
	for _, identity := range others {
		if !slices.Contains(admitted, identity) {
			return false, nil
		}
	}
	return true, secrets
}

func addsPackageMembers(previous api.Object, fields []string) bool {
	if len(fields) == 0 || len(fields) >= maxChangedPaths {
		return false
	}
	declared := previous.Spec().Get("packages")
	for _, field := range fields {
		member, ok := strings.CutPrefix(field, packagesPath+".")
		switch {
		case field == packagesPath && !declared.Present():
		case !ok || member == "" || strings.ContainsAny(member, ".[") || declared.Get(member).Present():
			return false
		}
	}
	return true
}

type notAnAddition struct{ error }

func (r notAnAddition) Unwrap() error { return r.error }

func refusal(err error) error {
	if err == nil {
		return nil
	}
	return notAnAddition{err}
}

func verdict(err error) error {
	if len(diagnostics.Of(err)) == 0 {
		return err
	}
	return refusal(err)
}

func isRefusal(err error) bool {
	var refused notAnAddition
	return errors.As(err, &refused)
}

func (d decider) additive(ctx context.Context, view View, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, proveCustody bool) (*additiveAdmission, error) {
	admission, err := d.addition(ctx, view, store, operation, frozen, states, proveCustody)
	var refused notAnAddition
	if errors.As(err, &refused) {
		return admission, refused.error
	}
	return admission, err
}

func (d decider) addition(ctx context.Context, view View, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, proveCustody bool) (*additiveAdmission, error) {
	if frozen.Preparation != nil || d.preparation != nil {
		return d.preparationAddition(ctx, view, store, operation, frozen, states, proveCustody)
	}
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone || inputDrift(view, operation) == "" {
		return nil, nil
	}
	before, after, ok := d.compiledBoth(ctx, view, operation)
	if !ok {
		return nil, nil
	}
	adds, addedSecrets := d.addsObjects(before.Effective(), after.Effective(), objectChanges(before.Effective(), after.Effective()))
	if !adds {
		return nil, nil
	}
	unfinished, err := unfinishedOver(ctx, store, operation, frozen, states)
	if err != nil {
		return nil, err
	}
	if len(unfinished) != 0 {
		return nil, refusal(applyRecordsMissing.Fail(
			"the completed apply "+operation.ID+" records no block completion for these blocks, so nothing proves it applied before it adds to it: "+strings.Join(unfinished, ", "),
			unfinishedApplyExits(view)))
	}
	if err := d.planner.refuseUnsupported(after, d.preparation); err != nil {
		return nil, refusal(err)
	}
	fresh, planned, err := d.planner.planFrom(ctx, view, after, d.preparation)
	if err != nil {
		return nil, verdict(err)
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: frozen, states: states})
	if err != nil {
		return nil, err
	}
	held, err := chain.plan()
	if err != nil {
		return nil, err
	}
	delta := reconciliation.Compare(held, fresh)
	if first, differs := delta.First(); differs && first.Change != reconciliation.Added {
		return nil, refusal(applyNotAnAddition.Fail(
			"this executable would freeze the block "+first.Block+" differently from the plan this apply froze, so the input is no addition to it",
			d.changeRemedy(view)+"; "+contextCommand(view.Identity().Name, "diff")+" shows each change"))
	}
	if delta.Count(reconciliation.Added) == 0 {
		return nil, nil
	}
	if err := d.refuseNewReservations(ctx, view, before, planned); err != nil {
		return nil, err
	}
	admission, err := d.admission(ctx, view, chain, held, fresh)
	if err != nil {
		return nil, err
	}
	admission.controller = planned.controller
	if err := d.refuseUnnamedSecrets(view, chain, admission, addedSecrets); err != nil {
		return nil, err
	}
	if !proveCustody {
		return admission, nil
	}
	if err := d.proofs.custodyProved(ctx, view, chain, admission.plan); err != nil {
		return nil, err
	}
	return admission, nil
}

func (d decider) refuseNewReservations(ctx context.Context, view View, before *compilation.State, after capabilityBinding) error {
	_, held, err := d.planner.planFrom(ctx, view, before, d.preparation)
	if err != nil {
		return verdict(err)
	}
	unheld := func(reservation infrastructureservices.HostReservation) bool {
		return !slices.ContainsFunc(held.reservations, func(other infrastructureservices.HostReservation) bool { return reflect.DeepEqual(reservation, other) })
	}
	unkeyed := func(claim hostKeyedClaim) bool {
		return !slices.ContainsFunc(held.hostKeyed, func(other hostKeyedClaim) bool { return reflect.DeepEqual(claim, other) })
	}
	if slices.ContainsFunc(after.reservations, unheld) || slices.ContainsFunc(after.hostKeyed, unkeyed) {
		return refusal(applyNotAnAddition.Fail("the added blocks need a host reservation or an SSH reservation, which an additive apply never takes", d.changeRemedy(view)))
	}
	return nil
}

func (d decider) refuseUnnamedSecrets(view View, chain lineage, admission *additiveAdmission, added []string) error {
	if len(added) == 0 {
		return nil
	}
	var held []string
	for _, owned := range chain.owned() {
		block := owned.Block
		capability, found := d.capabilities.Resolve(block.Kind, block.Implementation)
		referencer, names := capability.(SecretReferencer)
		if !found || !names {
			return refusal(applyNotAnAddition.Fail("the block "+block.ID+" cannot name the Secrets it references, so no Secret is added beside it", d.changeRemedy(view)))
		}
		references, err := referencer.SecretReferences(block)
		if err != nil {
			return err
		}
		held = append(held, references...)
	}
	for _, name := range added {
		if !slices.Contains(admission.secrets, name) || slices.Contains(held, name) {
			return refusal(applyNotAnAddition.Fail("the Secret "+name+" is added, and no added block names it for the first time, so the input is no addition to the completed apply", d.changeRemedy(view)))
		}
	}
	return nil
}

func (d decider) compiledBoth(ctx context.Context, view View, operation operationstore.Operation) (before, after *compilation.State, ok bool) {
	sources, err := view.Revision(ctx, operation.Revision)
	if err != nil || digestOf(sources) != operation.InputDigest {
		return nil, nil, false
	}
	if before, err = d.sources.compileSources(ctx, sources); err != nil {
		return nil, nil, false
	}
	if after, err = d.sources.compile(ctx, view); err != nil {
		return nil, nil, false
	}
	return before, after, true
}

func (d decider) admission(ctx context.Context, view View, chain lineage, held, fresh reconciliation.Plan) (*additiveAdmission, error) {
	added, err := reconciliation.AdditiveOver(fresh, chain.members()...)
	if err != nil {
		return nil, unremovable(chain.head().operation, err)
	}
	var references []string
	for _, block := range added.Blocks {
		named, err := d.admitted(ctx, held, block, view)
		if err != nil {
			return nil, err
		}
		references = joinBindings(references, named...)
	}
	slices.Sort(references)
	merged, err := chain.merged(added)
	if err != nil {
		return nil, unremovable(chain.head().operation, err)
	}
	return &additiveAdmission{plan: added, merged: merged, secrets: references}, nil
}

func (d decider) admitted(ctx context.Context, held reconciliation.Plan, block reconciliation.Block, view View) ([]string, error) {
	remedy := d.changeRemedy(view)
	refuse := func(why string) error {
		return refusal(applyNotAnAddition.Fail("the block "+block.ID+" cannot be added to the completed apply: "+why, remedy))
	}
	capability, found := d.capabilities.Resolve(block.Kind, block.Implementation)
	if !found {
		return nil, refuse("this executable offers no implementation for it")
	}
	admitter, admits := capability.(DeltaAdmitter)
	if !admits {
		return nil, refuse("its capability admits no addition to a completed apply")
	}
	if scoped, scopes := capability.(ApplyOnlyMaterial); !scopes || !scoped.ReadsMaterialOnlyInApply(block) {
		return nil, refuse("its capability may read bound Secrets outside its own apply attempts")
	}
	referencer, names := capability.(SecretReferencer)
	if !names {
		return nil, refuse("its capability cannot name the Secrets it binds")
	}
	if err := admitter.AdmitAdded(held, block); err != nil {
		return nil, verdict(err)
	}
	return referencer.SecretReferences(block)
}

func (s Service) custodyProved(ctx context.Context, view View, chain lineage, added reconciliation.Plan) error {
	if err := s.proveLineageContracts(chain); err != nil {
		return refusal(err)
	}
	for _, block := range added.Blocks {
		references, err := s.producedReferences(block)
		if err != nil {
			return verdict(err)
		}
		if len(references) == 0 {
			continue
		}
		lend, release, err := s.producedLender(block, chain.definitions(), false, s.producedInContext(view.Identity().Name))
		if err != nil {
			return refusal(applyNotAnAddition.Fail("the block "+block.ID+" cannot be added: "+firstMessage(err), s.changeRemedy(view)))
		}
		for _, reference := range references {
			material, err := lend(ctx, reference)
			material.Clear()
			if err != nil {
				release()
				return refusal(applyNotAnAddition.Fail("the block "+block.ID+" cannot be added: "+firstMessage(err), s.changeRemedy(view)))
			}
		}
		release()
	}
	return nil
}

func (s Service) changeRemedy(view View) string {
	name := view.Identity().Name
	return "take back what it owns with " + contextCommand(name, string(reconciliation.Destroy)) + ", then run " + s.preparationRetry(name, contextCommand(name, string(reconciliation.Apply)))
}

func (d decider) additiveTransition(view View, operation operationstore.Operation, admission *additiveAdmission, selection reconciliation.Selection) (operationPlan, error) {
	name := view.Identity().Name
	selection, err := bindSelection(name, admission.plan, selection)
	if err != nil {
		return operationPlan{}, err
	}
	if err := refuseStageBoundary(name, admission.plan, nil, selection); err != nil {
		return operationPlan{}, err
	}
	binding := capabilityBinding{controller: admission.controller, secrets: admission.secrets}
	if admission.preparedBinding != nil {
		binding = *admission.preparedBinding
	}
	return operationPlan{
		fresh: true, verb: reconciliation.Apply, plan: admission.plan, selection: selection,
		reserveAddition: admission.preparedBinding != nil,
		predecessor:     operation.ID, lineagePlan: admission.merged,
		binding: binding,
		basis:   basis{revision: view.Identity().Revision, input: inputDigest(view)},
	}, nil
}

func (s Service) prerequisitesRefusal(ctx context.Context, view View, operation operationstore.Operation) error {
	if operation.Verb != reconciliation.Apply || operation.Predecessor == "" {
		return nil
	}
	store := s.store(view)
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return err
	}
	chain, err := lineageOf(ctx, store, lineageMember{operation: operation, plan: plan})
	if err != nil {
		return err
	}
	if err := s.proveLineageContracts(chain); err != nil {
		return err
	}
	owned := chain.owners()
	for _, block := range plan.Blocks {
		for _, id := range block.Prerequisites {
			owner, _, held := owned.of(id)
			if !held {
				return applyPrerequisiteLost.Fail("no apply of the lineage of "+operation.ID+" holds the prerequisite "+id, "restore the context store")
			}
			if state := owner.states[id]; state != reconciliation.BlockDone {
				if state == "" {
					state = reconciliation.BlockPending
				}
				return applyPrerequisiteLost.Fail(
					"the prerequisite "+id+" of "+block.ID+", held by the apply "+owner.operation.ID+", no longer reads done: it reads "+string(state),
					"take back what the lineage owns with "+contextCommand(view.Identity().Name, string(reconciliation.Destroy)))
			}
		}
	}
	return nil
}

type candidateView struct {
	View
	inputs desiredstate.Sources
}

func (v candidateView) Inputs() desiredstate.Sources { return v.inputs }

func (s Service) AddsTo(ctx context.Context, name string, candidate desiredstate.Sources) (string, error) {
	if err := s.available(ctx); err != nil {
		return "", err
	}
	var adds string
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil || operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone {
			return err
		}
		frozen, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return err
		}
		states, _, err := blockRecords(ctx, store, operation.ID, frozen)
		if err != nil {
			return err
		}
		if added, _ := s.decider(nil).additive(ctx, candidateView{View: view, inputs: candidate}, store, operation, frozen, states, false); added != nil {
			adds = operation.ID
		}
		return nil
	})
	return adds, err
}
