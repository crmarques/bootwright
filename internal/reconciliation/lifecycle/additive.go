package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type additiveAdmission struct {
	plan       reconciliation.Plan
	merged     reconciliation.Plan
	secrets    []string
	controller string
}

const packagesPath = "spec.packages"

func addsBindings(before, after api.Catalog, changes []objectChange) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		kind, name, _ := strings.Cut(change.identity, "/")
		if kind != string(api.ClusterAddonBinding) {
			return false
		}
		switch change.change {
		case reconciliation.Added:
		case reconciliation.Changed:
			previous, found := before.Find(api.ClusterAddonBinding, name)
			if !found || !addsPackageMembers(previous, change.fields) {
				return false
			}
		default:
			return false
		}
	}
	return true
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

func (s Service) additive(ctx context.Context, view View, store recordReader, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) (*additiveAdmission, error) {
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone || inputDrift(view, operation) == "" {
		return nil, nil
	}
	before, after, ok := s.compiledBoth(ctx, view, operation)
	if !ok || !addsBindings(before.Effective(), after.Effective(), objectChanges(before.Effective(), after.Effective())) {
		return nil, nil
	}
	unfinished, err := unfinishedOver(ctx, store, operation, frozen, states)
	if err != nil {
		return nil, err
	}
	if len(unfinished) != 0 {
		return nil, failure("lifecycle.state",
			"the completed apply "+operation.ID+" records no block completion for these blocks, so nothing proves it applied before it adds to it: "+strings.Join(unfinished, ", "),
			unfinishedApplyExits(view))
	}
	if err := s.refuseUnsupported(after); err != nil {
		return nil, err
	}
	fresh, planned, err := s.planFrom(ctx, view, after, reconciliation.Apply)
	if err != nil {
		return nil, err
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
		return nil, failure("lifecycle.state",
			"this executable would freeze the block "+first.Block+" differently from the plan this apply froze, so the input is no addition to it",
			changeRemedy(view)+"; "+contextCommand(view.Identity().Name, "diff")+" shows each change")
	}
	if delta.Count(reconciliation.Added) == 0 {
		return nil, nil
	}
	admission, err := s.admission(ctx, view, chain, held, fresh)
	if err != nil {
		return nil, err
	}
	admission.controller = planned.controller
	if err := s.custodyProved(ctx, view, chain, admission.plan); err != nil {
		return nil, err
	}
	return admission, nil
}

func (s Service) compiledBoth(ctx context.Context, view View, operation operationstore.Operation) (before, after *compilation.State, ok bool) {
	sources, err := view.Revision(ctx, operation.Revision)
	if err != nil || digestOf(sources) != operation.InputDigest {
		return nil, nil, false
	}
	if before, err = s.compileSources(ctx, sources); err != nil {
		return nil, nil, false
	}
	if after, err = s.compile(ctx, view); err != nil {
		return nil, nil, false
	}
	return before, after, true
}

func (s Service) admission(ctx context.Context, view View, chain lineage, held, fresh reconciliation.Plan) (*additiveAdmission, error) {
	plans := make([]reconciliation.Plan, 0, len(chain))
	for _, member := range chain {
		plans = append(plans, member.plan)
	}
	added, err := reconciliation.Additive(fresh, plans...)
	if err != nil {
		return nil, unremovable(chain.head().operation, err)
	}
	var references []string
	for _, block := range added.Blocks {
		named, err := s.admitted(ctx, held, block, view)
		if err != nil {
			return nil, err
		}
		references = joinBindings(references, named...)
	}
	slices.Sort(references)
	merged, err := reconciliation.Lineage(append(plans, added)...)
	if err != nil {
		return nil, unremovable(chain.head().operation, err)
	}
	return &additiveAdmission{plan: added, merged: merged, secrets: references}, nil
}

func (s Service) admitted(ctx context.Context, held reconciliation.Plan, block reconciliation.Block, view View) ([]string, error) {
	remedy := changeRemedy(view)
	refuse := func(why string) error {
		return failure("lifecycle.state", "the block "+block.ID+" cannot be added to the completed apply: "+why, remedy)
	}
	capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
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
		return nil, err
	}
	return referencer.SecretReferences(block)
}

func (s Service) custodyProved(ctx context.Context, view View, chain lineage, added reconciliation.Plan) error {
	for _, block := range added.Blocks {
		references, err := s.producedReferences(block)
		if err != nil {
			return err
		}
		if len(references) == 0 {
			continue
		}
		lend, release, err := s.producedLender(block, chain.definitions(), false, s.producedInContext(view.Identity().Name))
		if err != nil {
			return failure("lifecycle.state", "the block "+block.ID+" cannot be added: "+firstMessage(err), changeRemedy(view))
		}
		for _, reference := range references {
			material, err := lend(ctx, reference)
			material.Clear()
			if err != nil {
				release()
				return failure("lifecycle.state", "the block "+block.ID+" cannot be added: "+firstMessage(err), changeRemedy(view))
			}
		}
		release()
	}
	return nil
}

func changeRemedy(view View) string {
	name := view.Identity().Name
	return "take back what it owns with " + contextCommand(name, string(reconciliation.Destroy)) + ", then run " + contextCommand(name, string(reconciliation.Apply))
}

func (s Service) additiveTransition(view View, operation operationstore.Operation, admission *additiveAdmission, selection reconciliation.Selection) (transition, error) {
	name := view.Identity().Name
	selection, err := bindSelection(name, admission.plan, selection)
	if err != nil {
		return transition{}, err
	}
	if err := refuseStageBoundary(name, admission.plan, nil, selection); err != nil {
		return transition{}, err
	}
	return transition{
		fresh: true, verb: reconciliation.Apply, plan: admission.plan, selection: selection,
		predecessor: operation.ID, lineagePlan: admission.merged,
		binding: capabilityBinding{controller: admission.controller, secrets: admission.secrets},
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
	for _, block := range plan.Blocks {
		for _, id := range block.Prerequisites {
			owner, held := chain.ownerOf(id)
			if !held {
				return failure("lifecycle.state", "no apply of the lineage of "+operation.ID+" holds the prerequisite "+id, "restore the context store")
			}
			if state := owner.states[id]; state != reconciliation.BlockDone {
				if state == "" {
					state = reconciliation.BlockPending
				}
				return failure("lifecycle.state",
					"the prerequisite "+id+" of "+block.ID+", held by the apply "+owner.operation.ID+", no longer reads done: it reads "+string(state),
					"take back what the lineage owns with "+contextCommand(view.Identity().Name, string(reconciliation.Destroy)))
			}
		}
	}
	return nil
}
