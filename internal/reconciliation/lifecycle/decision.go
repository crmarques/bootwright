package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type decider struct {
	preparation  *reconciliation.PreparationScope
	sources      *compilations
	operations   OperationStoreFactory
	capabilities CapabilityResolver
	preparations []PreparationDefinition
	planner      decisionPlanner
	proofs       decisionProofs
	relaxing     eraseRelaxing
	changeRemedy func(View) string
}

func (s Service) decider(scope *reconciliation.PreparationScope) decider {
	return decider{
		preparation: scope, sources: newCompilations(s.compiler), operations: s.options.Operations,
		capabilities: s.capabilities, preparations: s.options.Preparations, planner: s, proofs: s, relaxing: s.relaxing, changeRemedy: s.changeRemedy,
	}
}

func (d decider) store(view View) decisionRecords {
	return d.operations(view.Operations(), view.Logs())
}

func (d decider) preparationDefinition(kind string) (PreparationDefinition, bool) {
	return preparationDefinitionOf(d.preparations, kind)
}

// decideShared takes one decision from durable state read under the shared
// lock, together with the identity of the context it read.
func (s Service) decideShared(ctx context.Context, name string, verb reconciliation.Verb, selection reconciliation.Selection, scope *reconciliation.PreparationScope) (operationPlan, ContextIdentity, error) {
	var decided operationPlan
	var identity ContextIdentity
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		identity = view.Identity()
		decided, err = s.decider(scope).decide(ctx, view, verb, selection)
		return err
	})
	return decided, identity, err
}

// decide reads durable state and returns the one legal transition, planned
// from the context it read. Changed desired state never turns a continuation
// into a reconciliation. An operation whose finalization is due is marked
// before any other decision is taken.
func (d decider) decide(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, error) {
	decided, _, err := d.decideAs(ctx, view, verb, selection)
	return decided, err
}

// decideAs reports the verb it decided as; no verb chooses one, as a preview does.
func (d decider) decideAs(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, reconciliation.Verb, error) {
	decided, chosen, err := d.decideFrom(ctx, view, verb, selection)
	decided.basis.context = view.Identity().Name
	return d.withPhases(decided, err), chosen, err
}

func (d decider) withPhases(decided operationPlan, err error) operationPlan {
	if err == nil && decided.verb == reconciliation.Destroy {
		decided.phases = d.planner.computePhases(decided.plan)
	}
	return decided
}

func (d decider) decideFrom(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, reconciliation.Verb, error) {
	store := d.store(view)
	index, err := store.Index(ctx)
	if err != nil {
		return operationPlan{}, verb, err
	}
	if index.Current == "" {
		if verb == "" {
			verb = reconciliation.Apply
		}
		if classify(verb, standing{}) == transitionSettle {
			decided, err := unclaimed(ctx, view, store)
			return decided, verb, err
		}
		// A lost index reads as no operation, and an apply registered over it
		// would leave the effects its records name to nothing.
		if err := refuseUnindexed(ctx, view, store); err != nil {
			return operationPlan{}, verb, err
		}
		decided, err := d.freshApply(ctx, view, selection)
		return decided, verb, err
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return operationPlan{}, verb, err
	}
	frozen, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return operationPlan{}, verb, err
	}
	states, attempts, err := blockRecords(ctx, store, operation.ID, frozen)
	if err != nil {
		return operationPlan{}, verb, err
	}
	var known *additiveVerdict
	if verb == "" {
		held := standing{held: true, verb: operation.Verb, state: operation.State}
		admitted, refused := d.additive(ctx, view, store, operation, frozen, states, true)
		known = &additiveVerdict{admitted: admitted, refused: refused}
		held.additive = admitted != nil
		if verb = chosenVerb(held); verb == reconciliation.Destroy {
			selection = reconciliation.Selection{}
		}
	}
	if verb == reconciliation.Apply && d.preparation != nil && d.preparation.Command != string(reconciliation.Apply) &&
		frozen.Preparation != nil && frozen.Preparation.Command == string(reconciliation.Apply) {
		if err := d.admitPreparationCommand(frozen, operation); err != nil {
			return operationPlan{}, verb, err
		}
	}
	if marked, err := finalization(ctx, view, store, verb, operation, frozen, states, attempts); err != nil || marked.finalize {
		return marked, verb, err
	}
	decided, err := d.decideOver(ctx, view, store, verb, selection, operation, frozen, states, attempts, known)
	return decided, verb, err
}

// decideOver is the decision over a current operation whose finalization is
// not due. A preview also takes it over the record a due finalization would
// leave, so it decides as the verb does once that finalization is done.
func (d decider) decideOver(ctx context.Context, view View, store decisionRecords, verb reconciliation.Verb, selection reconciliation.Selection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int, known *additiveVerdict) (operationPlan, error) {
	decided, err := d.decideBy(ctx, view, store, verb, selection, operation, frozen, states, attempts, known)
	if err != nil {
		return decided, err
	}
	decided, err = withLineagePlan(ctx, store, decided, operation, frozen, states)
	if err == nil && decided.noop && verb == reconciliation.Apply && selection.Targeted() {
		plan := frozen
		if len(decided.lineagePlan.Blocks) != 0 {
			plan = decided.lineagePlan
		}
		err = checkTargets(view.Identity().Name, plan, selection)
	}
	return decided, err
}

type additiveVerdict struct {
	admitted *additiveAdmission
	refused  error
}

func (d decider) decideBy(ctx context.Context, view View, store decisionRecords, verb reconciliation.Verb, selection reconciliation.Selection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int, known *additiveVerdict) (operationPlan, error) {
	if verb == reconciliation.Apply {
		if err := d.admitPreparationCommand(frozen, operation); err != nil {
			return operationPlan{}, err
		}
	}
	name := view.Identity().Name
	held := standingOf(operation, frozen, states)
	var admitted *additiveAdmission
	var refused error
	if verb == reconciliation.Apply {
		if known == nil {
			admitted, refused = d.additive(ctx, view, store, operation, frozen, states, true)
		} else {
			admitted, refused = known.admitted, known.refused
		}
		if refused != nil && (d.preparation != nil || frozen.Preparation != nil) {
			return operationPlan{}, refused
		}
		held.additive = admitted != nil
	}
	switch classify(verb, held) {
	case transitionAdd:
		decided, err := d.additiveTransition(view, operation, admitted, selection)
		if err != nil {
			return operationPlan{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
	case transitionFinalize:
		return operationPlan{}, engineFinalizationDue.Fail(
			"the "+string(operation.Verb)+" "+operation.ID+" is due its finalization, which no decision over its record may skip",
			"report this as a defect, and repeat the command, which finalizes that operation before it decides")
	case transitionReplaceRemoval:
		return d.supersede(ctx, view, store, operation, frozen, states, attempts)
	case transitionRefuse:
		if operation.State != reconciliation.OperationDone {
			return operationPlan{}, d.refuseOver(ctx, view, operation, frozen, states)
		}
		return operationPlan{}, destroyRecordsMissing.Fail(
			"the completed destroy "+operation.ID+" records no block completion for these blocks, so nothing proves their effects removed: "+strings.Join(unfinishedBlocks(frozen, states), ", "),
			deletionExit(view))
	case transitionSettle:
		if operation.Verb == reconciliation.Apply {
			decided, err := d.settleApply(ctx, view, operation, frozen, states, refused)
			return plannedFrom(decided, operation, states, attempts), err
		}
		return settledDestroy(ctx, store, operationPlan{noop: true, verb: verb, operation: operation, plan: frozen, states: states}), nil
	case transitionFresh:
		if operation.Verb == reconciliation.Apply {
			scope, err := removalOver(ctx, store, operation, frozen, completedOwnership(frozen))
			if err != nil {
				return operationPlan{}, err
			}
			decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, operation.ID, scope, false)
			if err != nil {
				return operationPlan{}, err
			}
			return plannedFrom(decided, operation, states, attempts), nil
		}
		decided, err := d.freshApply(ctx, view, selection)
		if err != nil {
			return operationPlan{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
	}
	if verb == reconciliation.Destroy && operation.Verb == reconciliation.Destroy {
		switch d.relaxing.over(frozenRelaxations(frozen)) {
		case relaxationAdded:
			return d.supersede(ctx, view, store, operation, frozen, states, attempts)
		case relaxationDiffers:
			return operationPlan{}, refuseChangedRelaxations(name, operation, frozen, states)
		}
	}
	if err := refuseUncontinuable(ctx, store, name, operation, frozen); err != nil {
		return operationPlan{}, err
	}
	if verb == reconciliation.Apply {
		bound, err := bindSelection(name, frozen, selection)
		if err != nil {
			return operationPlan{}, err
		}
		if err := refuseStageBoundary(name, frozen, states, bound); err != nil {
			return operationPlan{}, err
		}
		selection = bound
	}
	decided := operationPlan{verb: verb, operation: operation, plan: frozen, states: states, source: operation.Source, selection: selection}
	if verb == reconciliation.Destroy && operation.Source != "" {
		released, err := sourceBindings(ctx, store, operation.Source)
		if err != nil {
			return operationPlan{}, err
		}
		decided.release = released
	}
	decided.executionProof = d.proofs.proveExecution(ctx, view, operation, frozen, states)
	return plannedFrom(decided, operation, states, attempts), nil
}

// refuseOver refuses a verb over an incomplete operation of the other verb,
// which only an apply over an incomplete removal reaches. It names that
// removal's continuation, or the fresh removal that replaces a failed one,
// only where the proof that command would make admits it: the continuation's
// re-proof, which rebuilds no runtime, or the replacement's host proof.
// Otherwise it names the exit that proof's refusal names, followed by that
// refusal. A finalization runs no block and is held to neither.
func (d decider) refuseOver(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	const message = "must be continued before another operation"
	name := view.Identity().Name
	next := nextAction(operation, frozen, states)
	var proof error
	switch {
	case next == string(reconciliation.Destroy):
		proof = d.proofs.verifyRemovalHost(ctx, view)
	case pendingRemains(frozen, states):
		proof = d.proofs.verifyContinuation(ctx, view, operation, d.proofs.proveExecution(ctx, view, operation, frozen, states), nil)
	}
	refused := diagnostics.Of(proof)
	if proof != nil && len(refused) == 0 {
		return proof
	}
	if len(refused) != 0 {
		return withCause(engineOtherVerbIncomplete.Fail("an incomplete "+string(operation.Verb)+" "+message, refused[0].Remediation), proof)
	}
	remediation := "continue it with "
	if next == string(reconciliation.Destroy) {
		remediation = "replace it with "
	}
	return engineOtherVerbIncomplete.Fail("an incomplete "+string(operation.Verb)+" "+message,
		remediation+continuationCommand(name, operation, frozen, states))
}

// completedOwnership is a completed apply's whole frozen plan: every block of
// it ran, so a block whose record reads anything but done is taken back too
// (Q93); proveRemovable resolves the unproved ones first.
func completedOwnership(frozen reconciliation.Plan) reconciliation.Plan {
	return frozen
}

// settleApply is an apply over a completed apply. A changed input refuses by
// naming what changed and the removal it needs. The unchanged input settles only
// while every block of the frozen plan is done, because the settled verb reports
// that completion: a block whose record reads anything else, a lost one included,
// contradicts it, and nothing then proves the input applied.
func (d decider) settleApply(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, additiveRefusal error) (operationPlan, error) {
	if inputDrift(view, operation) != "" {
		if additiveRefusal != nil {
			return operationPlan{}, additiveRefusal
		}
		return operationPlan{}, d.changedInput(ctx, view, operation)
	}
	unfinished, err := unfinishedOver(ctx, d.store(view), operation, frozen, states)
	if err != nil {
		return operationPlan{}, err
	}
	if len(unfinished) != 0 {
		return operationPlan{}, applyRecordsMissing.Fail(
			"the completed apply "+operation.ID+" records no block completion for these blocks, so this input cannot be proved applied: "+strings.Join(unfinished, ", "),
			unfinishedApplyExits(view))
	}
	return operationPlan{noop: true, verb: reconciliation.Apply, operation: operation, plan: frozen, states: states}, nil
}

// unfinishedApplyExits names the destroy of the whole frozen plan, or the deletion.
func unfinishedApplyExits(view View) string {
	return "take back its whole frozen plan with " + contextCommand(view.Identity().Name, string(reconciliation.Destroy)) +
		", which first resolves each unproved block, or " + deletionExit(view)
}

// unfinishedBlocks names each block of a frozen plan whose record reads
// anything but done, with the state it reads, in frozen order. A lost record
// reads as pending.
func unfinishedBlocks(frozen reconciliation.Plan, states map[string]reconciliation.BlockState) []string {
	unfinished := []string{}
	for _, block := range frozen.Blocks {
		state := states[block.ID]
		if state == reconciliation.BlockDone {
			continue
		}
		if state == "" {
			state = reconciliation.BlockPending
		}
		unfinished = append(unfinished, block.ID+" ("+string(state)+")")
	}
	return unfinished
}

const (
	driftRevision = "revision"
	driftBytes    = "bytes"
)

func inputDrift(view View, operation operationstore.Operation) string {
	identity := view.Identity()
	switch {
	case operation.Context != identity.Name || operation.Revision != identity.Revision:
		return driftRevision
	case operation.InputDigest != inputDigest(view):
		return driftBytes
	}
	return ""
}

func (d decider) changedInput(ctx context.Context, view View, operation operationstore.Operation) error {
	const message = "the desired state changed after this apply completed"
	name := view.Identity().Name
	remedy := d.changeRemedy(view)
	named, ok := d.namedChange(ctx, view, operation)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ok {
		return applyInputChanged.Fail(message, remedy)
	}
	return applyInputChanged.Fail(message+": "+named, remedy+"; "+contextCommand(name, "diff")+" shows each change")
}

func (d decider) namedChange(ctx context.Context, view View, operation operationstore.Operation) (string, bool) {
	sources, err := view.Revision(ctx, operation.Revision)
	if err != nil || digestOf(sources) != operation.InputDigest {
		return "", false
	}
	before, err := d.sources.compileSources(ctx, sources)
	if err != nil {
		return "", false
	}
	after, err := d.sources.compile(ctx, view)
	if err != nil {
		return "", false
	}
	if operation.InputDigest == inputDigest(view) {
		return "its input revision changed, though its bytes repeat the ones this apply froze", true
	}
	changes := objectChanges(before.Effective(), after.Effective())
	if len(changes) == 0 {
		return "its bytes changed, but it declares the same objects", true
	}
	return describeChanges(changes), true
}

// refuseStageBoundary refuses before any effect exactly when the selected
// stages admit no work, as admission's first choice decides it. It names the
// exact apply that would unblock the operation, the selection widened by the
// stage it needs, so a selection mistake is corrected rather than silently
// doing nothing. A stage expansion binds authorization to the work it admits.
// While any block is failed, the work is the retries the scheduler would
// start, so a selection that admits none of the failed blocks refuses whichever
// one comes first.
func refuseStageBoundary(contextName string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) error {
	if !selection.Stages.Selects(reconciliation.StageController) {
		for _, block := range plan.Blocks {
			if block.Stage == reconciliation.StageController && unproved(states[block.ID]) {
				return applyStageEmpty.Fail("the controller stage has an unproved effect",
					"resolve it with "+contextCommand(contextName, string(reconciliation.Apply), "--dry-run", "--stage", "controller"))
			}
		}
	}
	kind, admitted := firstAdmission(plan, states, selection, nil, nil, nil)
	if len(admitted) != 0 {
		return nil
	}
	if selection.Targeted() {
		return targetedBoundary(contextName, plan, states, selection, kind)
	}
	tokens := authorizing(authorizationPlan(plan, states, reconciliation.Selection{}))
	if selection.Until != "" {
		flags := tokens
		if len(selection.Stages) != 0 {
			flags = append([]string{"--stage", strings.Join(selection.Stages.Names(), ",")}, tokens...)
		}
		unstaged := selection
		unstaged.Stages = nil
		if kind == StepStart {
			if ready := reconciliation.Startable(plan, states, unstaged); len(ready) != 0 {
				return applyStageEmpty.Fail("the selected stages have nothing to start",
					"repeat "+widenedApply(contextName, plan, states, selection, ready[0].Stage))
			}
		}
		if kind == StepRetry {
			for _, block := range plan.Blocks {
				if states[block.ID] == reconciliation.BlockFailed && unstaged.Admits(block) {
					return applyStageExcludesRetry.Fail("the block this operation must retry is outside the selected stages",
						"repeat "+widenedApply(contextName, plan, states, selection, block.Stage))
				}
			}
		}
		return applyStageEmpty.Fail(
			"the add-on instance --until names and the blocks it requires have nothing to start under this selection",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply), flags...)+" without --until")
	}
	if kind == StepRetry {
		failed, _ := plan.Block(heldBehind(plan, states, nil))
		return applyStageExcludesRetry.Fail(
			"the block this operation must retry is outside the selected stages",
			"repeat "+widenedApply(contextName, plan, states, selection, failed.Stage))
	}
	ready := reconciliation.Ready(plan, states)
	if len(ready) == 0 {
		return applyStageEmpty.Fail("the selected stages have nothing to start",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply), tokens...)+" without --stage")
	}
	return applyStageEmpty.Fail(
		"the selected stages have nothing to start",
		"repeat "+widenedApply(contextName, plan, states, selection, ready[0].Stage))
}

func targetedBoundary(name string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection, kind string) error {
	unstaged := selection
	unstaged.Stages = nil
	unstaged, _ = unstaged.Bind(plan)
	ready := reconciliation.Ready(plan, states)
	for _, block := range plan.Blocks {
		if !unstaged.Admits(block) {
			continue
		}
		if kind == StepRetry && states[block.ID] == reconciliation.BlockFailed ||
			kind == StepStart && slices.ContainsFunc(ready, func(ready reconciliation.Block) bool { return ready.ID == block.ID }) {
			wider := selection
			wider.Stages = append(slices.Clone(selection.Stages), block.Stage)
			wider.Stages, _ = reconciliation.ParseStages(wider.Stages.Names())
			return applyStageEmpty.Fail("the selected stages leave prerequisite "+block.ID+" outside this invocation",
				"review "+contextCommand(name, "apply --dry-run", invocationFlags(wider, nil, nil)...))
		}
	}
	if kind == StepRetry {
		failed := heldBehind(plan, states, nil)
		return applyStageExcludesRetry.Fail("the selected targets cannot retry "+failed,
			"review the failed block and explicitly select its target with bootwright apply --dry-run")
	}
	return applyStageEmpty.Fail("the selected targets have no work that can start",
		"review the recorded status and select a target with pending work or its required shared targets")
}

func checkTargets(contextName string, plan reconciliation.Plan, selection reconciliation.Selection) error {
	if len(selection.Clusters) != 0 {
		for _, block := range plan.Blocks {
			if len(block.ClusterOwners) == 0 && clusterChild(block.BlockDefinition) {
				return applyStageEmpty.Fail("the frozen plan lacks cluster ownership for "+block.ID,
					"continue the recorded apply by stage, or destroy it and review a fresh apply with --clusters")
			}
		}
	}
	if missing := selection.MissingTargets(plan); len(missing) != 0 {
		return applyStageEmpty.Fail("the plan has no blocks for selected targets: "+strings.Join(missing, ", "),
			"inspect this context with "+contextCommand(contextName, "status")+" and select a target with planned work")
	}
	return nil
}

func bindSelection(contextName string, plan reconciliation.Plan, selection reconciliation.Selection) (reconciliation.Selection, error) {
	if err := checkTargets(contextName, plan, selection); err != nil {
		return selection, err
	}
	bound, ok := selection.Bind(plan)
	if ok {
		return bound, nil
	}
	if selection.Targeted() {
		return selection, applyUntilUnknown.Fail("the add-on named by --until is outside the selected targets or absent from the plan",
			"select its cluster and name a planned add-on instance")
	}
	candidates := reconciliation.UntilCandidates(plan)
	if len(candidates) == 0 {
		return selection, applyUntilUnknown.Fail("the plan has no add-on instance for --until to name",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply))+" without --until")
	}
	if len(candidates) > 8 {
		candidates = append(candidates[:8], "and "+strconv.Itoa(len(candidates)-8)+" more")
	}
	return selection, applyUntilUnknown.Fail("the plan has no add-on instance named by --until",
		"name one of: "+strings.Join(candidates, ", "))
}

// widenedApply is the apply whose selection adds the stage a refusal needs, in
// the canonical order --stage lists them, with the plan's authorizations.
func widenedApply(name string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection, needed reconciliation.Stage) string {
	stages := append(slices.Clone(selection.Stages), needed)
	selection.Stages, _ = reconciliation.ParseStages(stages.Names())
	bound, _ := selection.Bind(plan)
	return contextCommand(name, "apply", invocationFlags(bound, authorizing(authorizationPlan(plan, states, bound)), nil)...)
}

// freshApply is the one fresh-plan decision: apply registers the transition it
// returns and plan previews that same transition, so each refusal it makes is
// the one both verbs report before registration. It is pure, because a preview
// takes it too: it compiles the frozen input and plans, and binds, reserves and
// writes nothing.
func (d decider) freshApply(ctx context.Context, view View, selection reconciliation.Selection) (operationPlan, error) {
	state, err := d.sources.compile(ctx, view)
	if err != nil {
		return operationPlan{}, err
	}
	if err := d.planner.refuseUnsupported(state, d.preparation); err != nil {
		return operationPlan{}, err
	}
	plan, binding, err := d.planner.planFrom(ctx, view, state, d.preparation)
	if err != nil {
		return operationPlan{}, err
	}
	if len(plan.Blocks) == 0 {
		return operationPlan{}, applyNothingDeclared.Fail("the selected Environment declares nothing this executable would create", "declare a managed infrastructure service, or see "+supportedExample)
	}
	selection, err = bindSelection(view.Identity().Name, plan, selection)
	if err != nil {
		return operationPlan{}, err
	}
	if err := refuseStageBoundary(view.Identity().Name, plan, nil, selection); err != nil {
		return operationPlan{}, err
	}
	return operationPlan{
		fresh: true, verb: reconciliation.Apply, plan: plan, binding: binding, selection: selection,
		basis: basis{revision: view.Identity().Revision, input: inputDigest(view)},
	}, nil
}

// supersedable reports whether a fresh removal may replace an incomplete
// operation. Any apply that has not completed qualifies, because what it owns
// is every block it started and that set is the same whether a block stopped
// at a boundary, failed, or lost its outcome to an interruption. An unproved
// block is not an exception to that: the removal proves it before it registers
// anything, and a block it cannot prove refuses the removal there. An
// incomplete removal is continued instead, and only a failed one is replaced,
// because a removal that lost an outcome is resolved by repeating itself.
// Replacement is the road out of a repair that changes what a block freezes,
// since a continuation keeps its frozen blocks.
func supersedable(operation operationstore.Operation) bool {
	return replaces(operation.Verb, operation.State)
}

// mayHaveStartedNothing reports whether an operation's own state admits that it
// owns no effect. An apply registers before its first block starts, so one that
// stopped before starting any is still running or paused. A completed apply's
// blocks are all done, a failed or unknown apply holds the block that made it
// so or, lagging behind them, blocks that are all done, and a failed removal
// whose blocks are all done is finalized before any decision reaches here.
func mayHaveStartedNothing(operation operationstore.Operation) bool {
	return operation.Verb == reconciliation.Apply &&
		(operation.State == reconciliation.OperationRunning || operation.State == reconciliation.OperationPaused)
}

// supersede plans a fresh removal over what an incomplete operation still owns:
// the blocks an apply started, or the blocks a removal has not yet proved gone.
func (d decider) supersede(ctx context.Context, view View, store decisionRecords, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (operationPlan, error) {
	name := view.Identity().Name
	if operation.Verb == reconciliation.Apply {
		// An empty set the operation's own state cannot explain is refused by
		// freshDestroy first, whatever else the records say.
		owned, startedNothing := reconciliation.OwnedSubset(frozen, states), mayHaveStartedNothing(operation)
		if len(owned.Blocks) != 0 || startedNothing {
			if err := refuseContradictions(ctx, store, name, operation, frozen, states, attempts); err != nil {
				return operationPlan{}, err
			}
		}
		scope, err := removalOver(ctx, store, operation, frozen, owned)
		if err != nil {
			return operationPlan{}, err
		}
		decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, operation.ID, scope, startedNothing)
		if err != nil {
			return operationPlan{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
	}
	// A superseded removal is continued by nothing, so its own binding is
	// released beside the lineage's once the replacement completes. The one it
	// reopens is the removal's own, which differs from the lineage root's only
	// after an access-credential rebind.
	scope, err := replacementOf(ctx, store, operation, reconciliation.RemainingSubset(frozen, states))
	if err != nil {
		return operationPlan{}, err
	}
	decided, err := d.planner.freshDestroy(ctx, name, operation.Executable, operation.Source, scope, false)
	if err != nil {
		return operationPlan{}, err
	}
	decided.replacedPhases = d.planner.computePhases(frozen)
	return plannedFrom(decided, operation, states, attempts), nil
}

// plannedFrom records the durable state a transition was planned from. The
// decision is read under the shared lock and the effects run under the
// exclusive one, so the transition re-proves this before it registers. It
// keeps the input a fresh apply recorded compiling its plan.
func plannedFrom(decided operationPlan, operation operationstore.Operation, states map[string]reconciliation.BlockState, attempts map[string]int) operationPlan {
	planned := recorded(operation, states, attempts)
	decided.basis.operation, decided.basis.state, decided.basis.record = planned.operation, planned.state, planned.record
	decided.basis.blocks, decided.basis.attempts = planned.blocks, planned.attempts
	return decided
}

// recorded is the basis an operation's records give, sharing nothing with
// them. Its bindings stay an empty set rather than none, because the record
// it is compared with decodes an empty set that way.
func recorded(operation operationstore.Operation, states map[string]reconciliation.BlockState, attempts map[string]int) basis {
	record := operation
	record.Bindings = slices.Clone(operation.Bindings)
	return basis{
		operation: operation.ID, state: operation.State, record: record,
		blocks: maps.Clone(states), attempts: maps.Clone(attempts),
	}
}

// blockRecords reads each frozen block's record: the state it proves and the
// attempts it counts. A block retried by another invocation that failed again
// leaves the state it found, so only its count shows the retry. A lost record
// reads as pending with no attempt.
func blockRecords(ctx context.Context, store recordReader, id string, frozen reconciliation.Plan) (map[string]reconciliation.BlockState, map[string]int, error) {
	states := make(map[string]reconciliation.BlockState, len(frozen.Blocks))
	attempts := make(map[string]int, len(frozen.Blocks))
	for _, block := range frozen.Blocks {
		record, err := store.Block(ctx, id, block.ID)
		if err != nil {
			return nil, nil, err
		}
		states[block.ID], attempts[block.ID] = record.State, record.Attempts
	}
	return states, attempts, nil
}

func firstBinding(bindings []string) string {
	if len(bindings) == 0 {
		return ""
	}
	return bindings[0]
}

// inputDigest binds the operation to the exact frozen bytes it planned from,
// independently of the revision identity the registry records.
func inputDigest(view View) string {
	return digestOf(view.Inputs())
}

func digestOf(sources desiredstate.Sources) string {
	parts := make([]string, 0, len(sources.Files)+len(sources.Markers))
	for _, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		for _, file := range collection {
			content := sha256.Sum256(file.Bytes())
			parts = append(parts, file.Path()+"\x00"+hex.EncodeToString(content[:]))
		}
	}
	slices.Sort(parts)
	digest := sha256.Sum256([]byte("bootwright.reconciliation.input-v1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
