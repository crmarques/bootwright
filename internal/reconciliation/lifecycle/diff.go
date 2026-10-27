package lifecycle

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	diffNone      = "none"
	diffUnchanged = "unchanged"
	diffChanged   = "changed"
	diffSame      = "same"
	diffDiffers   = "differs"
	diffUnplanned = "unplanned"
	diffReadable  = "readable"
	sideApplied   = "applied"
	sidePresent   = "present"
	sideAbsent    = "absent"
	sideUnknown   = "unknown"
)

type diffRead struct {
	context       ContextIdentity
	operation     operationstore.Operation
	plan          reconciliation.Plan
	states        map[string]reconciliation.BlockState
	realized      map[string]RealizationStatus
	owning        operationstore.Operation
	owningPlan    reconciliation.Plan
	owningStates  map[string]reconciliation.BlockState
	desired       api.Catalog
	applied       api.Catalog
	appliedReason string
	input         string
	fresh         reconciliation.Plan
	planReason    string
	delta         reconciliation.Delta
	rebuilt       []string
	basis         basis
	binding       string
	digest        string
	observed      bool
	blocks        DiffBlocks
	sides         map[string]observedSide
	unproven      map[api.Kind]bool
}

func (s Service) Diff(ctx context.Context, request DiffRequest) (*DiffResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	var read diffRead
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		value, err := s.readDiff(ctx, view)
		read = value
		return err
	})
	if err != nil {
		return nil, err
	}
	if request.Observe {
		if err := s.observeDiff(ctx, name, &read); err != nil {
			return nil, err
		}
	}
	return read.result(), nil
}

func (s Service) readDiff(ctx context.Context, view View) (diffRead, error) {
	var store recordReader = s.store(view)
	read := diffRead{context: view.Identity()}
	index, err := store.Index(ctx)
	if errors.Is(err, operationstore.ErrRetiredIndex) {
		return diffRead{}, operationstore.RetiredIndexRefusal(read.context.Name)
	}
	if err != nil {
		return diffRead{}, err
	}
	var attempts map[string]int
	if index.Current != "" {
		if attempts, err = read.readCurrent(ctx, store, index.Current); err != nil {
			return diffRead{}, err
		}
	}
	read.owning, read.owningPlan, read.owningStates, err = s.owningApply(ctx, store, read.operation, read.plan, read.states)
	if err != nil {
		return diffRead{}, err
	}
	desired, err := s.compile(ctx, view)
	if err != nil {
		return diffRead{}, err
	}
	read.desired = desired.Effective()
	applied, frozen, reason := s.appliedCatalog(ctx, view, read.owning, desired)
	if err := ctx.Err(); err != nil {
		return diffRead{}, err
	}
	read.applied, read.appliedReason = applied, reason
	read.fresh, read.planReason = s.freshPlan(ctx, view, desired)
	read.delta = reconciliation.Compare(read.owningPlan, read.fresh)
	read.rebuilt = s.rebuiltBlocks(ctx, view, read.owning, read.owningPlan, frozen)
	if err := ctx.Err(); err != nil {
		return diffRead{}, err
	}
	read.input = inputState(view, read.operation, read.owning)
	read.basis = recorded(read.operation, read.states, attempts)
	read.binding = firstBinding(read.owning.Bindings)
	read.digest = inputDigest(view)
	return read, nil
}

func (r *diffRead) readCurrent(ctx context.Context, store recordReader, id string) (map[string]int, error) {
	operation, err := store.ReadOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return nil, err
	}
	states, attempts, err := blockRecords(ctx, store, operation.ID, plan)
	if err != nil {
		return nil, err
	}
	realized, err := realizations(ctx, store, operation, plan, states)
	if err != nil {
		return nil, err
	}
	r.operation, r.plan, r.states, r.realized = operation, plan, states, realized
	return attempts, nil
}

func (s Service) owningApply(ctx context.Context, store recordReader, current operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) (operationstore.Operation, reconciliation.Plan, map[string]reconciliation.BlockState, error) {
	switch {
	case current.ID == "":
		return operationstore.Operation{}, reconciliation.Plan{}, nil, nil
	case current.Verb == reconciliation.Apply:
		return current, plan, states, nil
	case current.Source == "":
		return operationstore.Operation{}, reconciliation.Plan{}, nil, nil
	}
	applied, err := store.ReadOperation(ctx, current.Source)
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	frozen, err := store.ReadPlan(ctx, applied.ID)
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	reached, err := store.BlockStates(ctx, applied.ID, frozen)
	if err != nil {
		return operationstore.Operation{}, reconciliation.Plan{}, nil, err
	}
	return applied, frozen, reached, nil
}

func (s Service) appliedCatalog(ctx context.Context, view View, owning operationstore.Operation, desired *compilation.State) (api.Catalog, desiredstate.Sources, string) {
	if owning.ID == "" {
		return api.Catalog{}, desiredstate.Sources{}, ""
	}
	unreadable := func(detail string) (api.Catalog, desiredstate.Sources, string) {
		return api.Catalog{}, desiredstate.Sources{}, "the input revision " + owning.Revision + " that apply " + owning.ID + " froze cannot be read: " + detail
	}
	sources, err := view.Revision(ctx, owning.Revision)
	if err != nil {
		return unreadable(firstMessage(err))
	}
	if digestOf(sources) != owning.InputDigest {
		return unreadable("its bytes no longer match the input digest apply " + owning.ID + " recorded")
	}
	if owning.InputDigest == inputDigest(view) {
		return desired.Effective(), sources, ""
	}
	state, err := s.compileSources(ctx, sources)
	if err != nil {
		return unreadable(firstMessage(err))
	}
	return state.Effective(), sources, ""
}

func (s Service) freshPlan(ctx context.Context, view View, desired *compilation.State) (reconciliation.Plan, string) {
	plan, err := s.planSelected(ctx, view, desired)
	if err != nil {
		return reconciliation.Plan{}, firstMessage(err)
	}
	return plan, ""
}

func (s Service) planSelected(ctx context.Context, view View, state *compilation.State) (reconciliation.Plan, error) {
	if err := s.refuseUnsupported(state); err != nil {
		return reconciliation.Plan{}, err
	}
	plan, _, err := s.planFrom(ctx, view, state, reconciliation.Apply)
	return plan, err
}

func (s Service) rebuiltBlocks(ctx context.Context, view View, owning operationstore.Operation, owningPlan reconciliation.Plan, frozen desiredstate.Sources) []string {
	if owning.ID == "" || !s.otherBuild(owning) {
		return nil
	}
	replanned := reconciliation.Plan{}
	if len(frozen.Files) != 0 {
		if state, err := s.compileSources(ctx, frozen); err == nil {
			if plan, err := s.planSelected(ctx, view, state); err == nil {
				replanned = plan
			}
		}
	}
	var rebuilt []string
	for _, change := range reconciliation.Compare(owningPlan, replanned) {
		if change.Change == reconciliation.Removed || change.Change == reconciliation.Changed {
			rebuilt = append(rebuilt, change.Block)
		}
	}
	return rebuilt
}

func inputState(view View, current, owning operationstore.Operation) string {
	switch {
	case owning.ID == "", current.Verb == reconciliation.Destroy && current.State == reconciliation.OperationDone:
		return diffNone
	case inputDrift(view, owning) == "":
		return diffUnchanged
	}
	return diffChanged
}

func firstMessage(err error) string {
	if reported := diagnostics.Of(err); len(reported) != 0 {
		return reported[0].Message
	}
	return err.Error()
}

func (r diffRead) planState() string {
	switch {
	case r.planReason != "":
		return diffUnplanned
	case r.owning.ID == "":
		return diffNone
	}
	if _, differs := r.delta.First(); differs {
		return diffDiffers
	}
	return diffSame
}

func (r diffRead) result() *DiffResult {
	plan := DiffPlan{State: r.planState(), Reason: r.planReason, Rebuilt: append([]string{}, r.rebuilt...)}
	if plan.State == diffSame || plan.State == diffDiffers {
		plan.Changed, plan.Added, plan.Removed = r.delta.Count(reconciliation.Changed), r.delta.Count(reconciliation.Added), r.delta.Count(reconciliation.Removed)
	}
	operation := DiffOperation{ID: r.operation.ID, Verb: string(r.operation.Verb), State: string(r.operation.State), Apply: r.owning.ID, Revision: r.owning.Revision}
	if r.owning.ID != "" {
		operation.Applied = cmp.Or(r.appliedReason, diffReadable)
	}
	return &DiffResult{Context: r.context, Operation: operation, Input: r.input, Plan: plan, Observed: r.observed, Blocks: r.blocks, Objects: joinObjects(r)}
}

type objectPlan struct {
	change string
	blocks []string
}

func (r diffRead) objectPlans() map[string]objectPlan {
	plans := map[string]objectPlan{}
	if r.planReason == "" {
		for _, object := range r.delta.Objects() {
			plans[object.Kind+"/"+object.Object] = objectPlan{change: string(object.Change), blocks: object.Blocks}
		}
		return plans
	}
	for _, block := range r.owningPlan.Blocks {
		identity := block.Kind + "/" + block.Object
		plans[identity] = objectPlan{blocks: append(plans[identity].blocks, block.ID)}
	}
	return plans
}

func joinObjects(read diffRead) []ObjectDiff {
	plans := read.objectPlans()
	identities := catalogIdentities(read.desired, read.applied)
	for _, key := range slices.Concat(mapKeys(plans), mapKeys(read.realized), mapKeys(read.sides)) {
		kind, name, _ := strings.Cut(key, "/")
		if identity := (objectIdentity{kind: api.Kind(kind), name: name}); !slices.Contains(identities, identity) {
			identities = append(identities, identity)
		}
	}
	slices.SortFunc(identities, compareIdentities)
	objects := make([]ObjectDiff, 0, len(identities))
	for _, identity := range identities {
		key := string(identity.kind) + "/" + identity.name
		object := ObjectDiff{
			Kind: identity.kind, Name: identity.name, Plan: plans[key].change, Blocks: slices.Clone(plans[key].blocks),
			Realization: read.realized[key], Real: recordedSide(read, key),
		}
		if declared, found := read.desired.Find(identity.kind, identity.name); found {
			object.Desired = canonicalObject(declared)
		}
		if side, found := read.sides[key]; found {
			object.Real = side.real
			if side.pruned {
				object.Desired = side.desired
			}
		}
		if object.Desired == nil && object.Real.State == sideAbsent {
			continue
		}
		objects = append(objects, object)
	}
	return objects
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func recordedSide(read diffRead, identity string) RealSide {
	side := RealSide{Source: sideApplied, State: sideAbsent}
	current := read.operation
	status, named := read.realized[identity]
	switch {
	case read.owning.ID == "":
		side.Reason = "no operation of this context realized it"
	case named && status == RealizationDone:
		return read.appliedSide(identity, true)
	case named && status == RealizationPending && current.Verb == reconciliation.Destroy:
		side.Reason = "destroy " + current.ID + " took it back"
	case named && status == RealizationPending:
		side.Reason = "apply " + current.ID + " has not realized it"
	case named && current.Verb == reconciliation.Destroy && read.partlyRemoved(identity):
		removed, remaining := read.removalOf(identity)
		side.State = sideUnknown
		side.Reason = "destroy " + current.ID + " has removed " + strings.Join(removed, ", ") + " and not yet " + strings.Join(remaining, ", ")
	case named:
		side.State, side.Remedy = sideUnknown, reviewStatus(read.context.Name)
		side.Reason = "its blocks read " + string(status) + ": " + strings.Join(read.blocksOf(identity), ", ")
	case current.Verb == reconciliation.Destroy && current.State == reconciliation.OperationDone:
		side.Reason = "destroy " + current.ID + " took it back"
	default:
		return read.appliedSide(identity, false)
	}
	return side
}

func (r diffRead) appliedSide(identity string, realized bool) RealSide {
	side := RealSide{Source: sideApplied, State: sidePresent}
	if r.appliedReason != "" {
		side.State, side.Reason = sideUnknown, r.appliedReason
		return side
	}
	kind, name, _ := strings.Cut(identity, "/")
	applied, found := r.applied.Find(api.Kind(kind), name)
	switch {
	case found:
		side.Object = canonicalObject(applied)
	case realized:
		side.State, side.Remedy = sideUnknown, reviewStatus(r.context.Name)
		side.Reason = "the input revision " + r.owning.Revision + " that apply " + r.owning.ID + " froze does not declare it"
	default:
		side.State, side.Reason = sideAbsent, "apply "+r.owning.ID+" did not declare it"
	}
	return side
}

// partlyRemoved reports a removal that proved some of an object's blocks gone
// and has not yet run the others, which proves nothing of the object as a whole.
func (r diffRead) partlyRemoved(identity string) bool {
	removed, remaining := r.removalOf(identity)
	return len(removed) != 0 && len(remaining) != 0 && !slices.ContainsFunc(r.plan.Blocks, func(block reconciliation.Block) bool {
		state := r.states[block.ID]
		return block.Kind+"/"+block.Object == identity && (unproved(state) || state == reconciliation.BlockFailed)
	})
}

func (r diffRead) removalOf(identity string) ([]string, []string) {
	var removed, remaining []string
	for _, block := range r.plan.Blocks {
		switch {
		case block.Kind+"/"+block.Object != identity:
		case r.states[block.ID] == reconciliation.BlockDone:
			removed = append(removed, block.ID)
		default:
			remaining = append(remaining, block.ID)
		}
	}
	return removed, remaining
}

func (r diffRead) blocksOf(identity string) []string {
	var unsettled, named []string
	for _, block := range r.plan.Blocks {
		if block.Kind+"/"+block.Object != identity {
			continue
		}
		named = append(named, block.ID)
		if r.states[block.ID] != reconciliation.BlockDone {
			unsettled = append(unsettled, block.ID)
		}
	}
	if len(unsettled) != 0 {
		return unsettled
	}
	if len(named) != 0 {
		return named
	}
	for _, block := range r.owningPlan.Blocks {
		if block.Kind+"/"+block.Object == identity {
			named = append(named, block.ID)
		}
	}
	return named
}
