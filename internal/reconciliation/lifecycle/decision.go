package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// decideShared takes one decision from durable state read under the shared
// lock, together with the identity of the context it read.
func (s Service) decideShared(ctx context.Context, name string, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, ContextIdentity, error) {
	var decided operationPlan
	var identity ContextIdentity
	err := s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		identity = view.Identity()
		decided, err = s.decide(ctx, view, verb, selection)
		return err
	})
	return decided, identity, err
}

// decide reads durable state and returns the one legal transition, planned
// from the context it read. Changed desired state never turns a continuation
// into a reconciliation. An operation whose finalization is due is marked
// before any other decision is taken.
func (s Service) decide(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, error) {
	decided, _, err := s.decideAs(ctx, view, verb, selection)
	return decided, err
}

// decideAs reports the verb it decided as; no verb chooses one, as a preview does.
func (s Service) decideAs(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, reconciliation.Verb, error) {
	decided, chosen, err := s.decideFrom(ctx, view, verb, selection)
	decided.basis.context = view.Identity().Name
	return s.withPhases(decided, err), chosen, err
}

func (s Service) withPhases(decided operationPlan, err error) operationPlan {
	if err == nil && decided.verb == reconciliation.Destroy {
		decided.phases = s.computePhases(decided.plan)
	}
	return decided
}

func (s Service) decideFrom(ctx context.Context, view View, verb reconciliation.Verb, selection reconciliation.Selection) (operationPlan, reconciliation.Verb, error) {
	store := s.store(view)
	index, err := store.Index(ctx)
	if errors.Is(err, operationstore.ErrRetiredIndex) {
		return operationPlan{}, verb, operationstore.RetiredIndexRefusal(view.Identity().Name)
	}
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
		decided, err := s.freshApply(ctx, view, selection)
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
	if verb == "" {
		held := standing{held: true, verb: operation.Verb, state: operation.State}
		admitted, _ := s.additive(ctx, view, store, operation, frozen, states, true)
		held.additive = admitted != nil
		if verb = chosenVerb(held); verb == reconciliation.Destroy {
			selection = reconciliation.Selection{}
		}
	}
	if marked, err := finalization(ctx, view, store, verb, operation, frozen, states, attempts); err != nil || marked.finalize {
		return marked, verb, err
	}
	decided, err := s.decideOver(ctx, view, store, verb, selection, operation, frozen, states, attempts)
	return decided, verb, err
}

// decideOver is the decision over a current operation whose finalization is
// not due. A preview also takes it over the record a due finalization would
// leave, so it decides as the verb does once that finalization is done.
func (s Service) decideOver(ctx context.Context, view View, store OperationStore, verb reconciliation.Verb, selection reconciliation.Selection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (operationPlan, error) {
	decided, err := s.decideBy(ctx, view, store, verb, selection, operation, frozen, states, attempts)
	if err != nil {
		return decided, err
	}
	return withLineagePlan(ctx, store, decided, operation, frozen, states)
}

func (s Service) decideBy(ctx context.Context, view View, store OperationStore, verb reconciliation.Verb, selection reconciliation.Selection, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (operationPlan, error) {
	name := view.Identity().Name
	held := standingOf(operation, frozen, states)
	var admitted *additiveAdmission
	var refused error
	if verb == reconciliation.Apply {
		admitted, refused = s.additive(ctx, view, store, operation, frozen, states, true)
		held.additive = admitted != nil
	}
	switch classify(verb, held) {
	case transitionAdd:
		decided, err := s.additiveTransition(view, operation, admitted, selection)
		if err != nil {
			return operationPlan{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
	case transitionFinalize:
		return operationPlan{}, failure("lifecycle.state",
			"the "+string(operation.Verb)+" "+operation.ID+" is due its finalization, which no decision over its record may skip",
			"report this as a defect, and repeat the command, which finalizes that operation before it decides")
	case transitionReplaceRemoval:
		return s.supersede(ctx, view, store, operation, frozen, states, attempts)
	case transitionRefuse:
		if operation.State != reconciliation.OperationDone {
			return operationPlan{}, s.refuseOver(ctx, view, operation, frozen, states)
		}
		return operationPlan{}, failure("lifecycle.state",
			"the completed destroy "+operation.ID+" records no block completion for these blocks, so nothing proves their effects removed: "+strings.Join(unfinishedBlocks(frozen, states), ", "),
			deletionExit(view))
	case transitionSettle:
		if operation.Verb == reconciliation.Apply {
			decided, err := s.settleApply(ctx, view, operation, frozen, states, refused)
			return plannedFrom(decided, operation, states, attempts), err
		}
		return settledDestroy(ctx, store, operationPlan{noop: true, verb: verb, operation: operation, plan: frozen, states: states}), nil
	case transitionFresh:
		if operation.Verb == reconciliation.Apply {
			scope, err := removalOver(ctx, store, operation, frozen, completedOwnership(frozen))
			if err != nil {
				return operationPlan{}, err
			}
			decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.ID, scope, false)
			if err != nil {
				return operationPlan{}, err
			}
			return plannedFrom(decided, operation, states, attempts), nil
		}
		decided, err := s.freshApply(ctx, view, selection)
		if err != nil {
			return operationPlan{}, err
		}
		return plannedFrom(decided, operation, states, attempts), nil
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
	decided.sameBlocks = s.proveSameBlocks(ctx, view, operation, frozen, states)
	return plannedFrom(decided, operation, states, attempts), nil
}

// refuseOver refuses a verb over an incomplete operation of the other verb,
// which only an apply over an incomplete removal reaches. It names that
// removal's continuation, or the fresh removal that replaces a failed one,
// only where the proof that command would make admits it: the continuation's
// re-proof, which rebuilds no runtime, or the replacement's host proof.
// Otherwise it names the exit that proof's refusal names, followed by that
// refusal. A finalization runs no block and is held to neither.
func (s Service) refuseOver(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	const message = "must be continued before another operation"
	name := view.Identity().Name
	next := nextAction(operation, frozen, states)
	var proof error
	switch {
	case next == string(reconciliation.Destroy):
		proof = s.verifyRemovalHost(ctx, view)
	case pendingRemains(frozen, states):
		proof = s.verifyContinuation(ctx, view, operation, s.proveSameBlocks(ctx, view, operation, frozen, states), nil)
	}
	refused := diagnostics.Of(proof)
	if proof != nil && len(refused) == 0 {
		return proof
	}
	if len(refused) != 0 {
		return withCause(failure("lifecycle.state", "an incomplete "+string(operation.Verb)+" "+message, refused[0].Remediation), proof)
	}
	remediation := "continue it with "
	if next == string(reconciliation.Destroy) {
		remediation = "replace it with "
	}
	return failure("lifecycle.state", "an incomplete "+string(operation.Verb)+" "+message,
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
func (s Service) settleApply(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, additiveRefusal error) (operationPlan, error) {
	if inputDrift(view, operation) != "" {
		if additiveRefusal != nil {
			return operationPlan{}, additiveRefusal
		}
		return operationPlan{}, s.changedInput(ctx, view, operation)
	}
	unfinished, err := unfinishedOver(ctx, s.store(view), operation, frozen, states)
	if err != nil {
		return operationPlan{}, err
	}
	if len(unfinished) != 0 {
		return operationPlan{}, failure("lifecycle.state",
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

func (s Service) changedInput(ctx context.Context, view View, operation operationstore.Operation) error {
	const message = "the desired state changed after this apply completed"
	name := view.Identity().Name
	remedy := changeRemedy(view)
	named, ok := s.namedChange(ctx, view, operation)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ok {
		return failure("lifecycle.state", message, remedy)
	}
	return failure("lifecycle.state", message+": "+named, remedy+"; "+contextCommand(name, "diff")+" shows each change")
}

func (s Service) namedChange(ctx context.Context, view View, operation operationstore.Operation) (string, bool) {
	sources, err := view.Revision(ctx, operation.Revision)
	if err != nil || digestOf(sources) != operation.InputDigest {
		return "", false
	}
	before, err := s.compileSources(ctx, sources)
	if err != nil {
		return "", false
	}
	after, err := s.compile(ctx, view)
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
// doing nothing. That apply is authorized against the whole plan, so it
// carries every token the plan consumes and passes authorization as named.
// While any block is failed, the work is the retries the scheduler would
// start, so a selection that admits none of the failed blocks refuses whichever
// one comes first.
func refuseStageBoundary(contextName string, plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection) error {
	kind, admitted := firstAdmission(plan, states, selection, nil, nil, nil)
	if len(admitted) != 0 {
		return nil
	}
	tokens := authorizing(plan)
	if selection.Until != "" {
		flags := tokens
		if len(selection.Stages) != 0 {
			flags = append([]string{"--stage", strings.Join(selection.Stages.Names(), ",")}, tokens...)
		}
		unstaged := selection
		unstaged.Stages = nil
		if kind == StepStart {
			if ready := reconciliation.Startable(plan, states, unstaged); len(ready) != 0 {
				return failure("lifecycle.stage", "the selected stages have nothing to start",
					"repeat "+widenedApply(contextName, selection, ready[0].Stage, append([]string{"--until", selection.Until}, tokens...)))
			}
		}
		if kind == StepRetry {
			for _, block := range plan.Blocks {
				if states[block.ID] == reconciliation.BlockFailed && unstaged.Admits(block) {
					return failure("lifecycle.stage", "the block this operation must retry is outside the selected stages",
						"repeat "+widenedApply(contextName, selection, block.Stage, append([]string{"--until", selection.Until}, tokens...)))
				}
			}
		}
		return failure("lifecycle.stage",
			"the add-on instance --until names and the blocks it requires have nothing to start under this selection",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply), flags...)+" without --until")
	}
	if kind == StepRetry {
		failed, _ := plan.Block(heldBehind(plan, states, nil))
		return failure("lifecycle.stage",
			"the block this operation must retry is outside the selected stages",
			"repeat "+widenedApply(contextName, selection, failed.Stage, tokens))
	}
	ready := reconciliation.Ready(plan, states)
	if len(ready) == 0 {
		return failure("lifecycle.stage", "the selected stages have nothing to start",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply), tokens...)+" without --stage")
	}
	return failure("lifecycle.stage",
		"the selected stages have nothing to start",
		"repeat "+widenedApply(contextName, selection, ready[0].Stage, tokens))
}

func bindSelection(contextName string, plan reconciliation.Plan, selection reconciliation.Selection) (reconciliation.Selection, error) {
	bound, ok := selection.Bind(plan)
	if ok {
		return bound, nil
	}
	candidates := reconciliation.UntilCandidates(plan)
	if len(candidates) == 0 {
		return selection, failure("lifecycle.stage", "the plan has no add-on instance for --until to name",
			"repeat "+contextCommand(contextName, string(reconciliation.Apply))+" without --until")
	}
	if len(candidates) > 8 {
		candidates = append(candidates[:8], "and "+strconv.Itoa(len(candidates)-8)+" more")
	}
	return selection, failure("lifecycle.stage", "the plan has no add-on instance named by --until",
		"name one of: "+strings.Join(candidates, ", "))
}

// widenedApply is the apply whose selection adds the stage a refusal needs, in
// the canonical order --stage lists them, with the plan's authorizations.
func widenedApply(contextName string, selection reconciliation.Selection, needed reconciliation.Stage, tokens []string) string {
	stages := slices.Clone(selection.Stages)
	if !slices.Contains(stages, needed) {
		stages = append(stages, needed)
	}
	slices.SortStableFunc(stages, func(x, y reconciliation.Stage) int {
		return slices.Index(reconciliation.Stages(), x) - slices.Index(reconciliation.Stages(), y)
	})
	return contextCommand(contextName, string(reconciliation.Apply), append([]string{"--stage", strings.Join(stages.Names(), ",")}, tokens...)...)
}

// freshApply is the one fresh-plan decision: apply registers the transition it
// returns and plan previews that same transition, so each refusal it makes is
// the one both verbs report before registration. It is pure, because a preview
// takes it too: it compiles the frozen input and plans, and binds, reserves and
// writes nothing.
func (s Service) freshApply(ctx context.Context, view View, selection reconciliation.Selection) (operationPlan, error) {
	state, err := s.compile(ctx, view)
	if err != nil {
		return operationPlan{}, err
	}
	if err := s.refuseUnsupported(state); err != nil {
		return operationPlan{}, err
	}
	plan, binding, err := s.planFrom(ctx, view, state)
	if err != nil {
		return operationPlan{}, err
	}
	if len(plan.Blocks) == 0 {
		return operationPlan{}, failure("lifecycle.state", "the selected Environment declares nothing this executable would create", "declare a managed infrastructure service, or see "+supportedExample)
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
func (s Service) supersede(ctx context.Context, view View, store OperationStore, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState, attempts map[string]int) (operationPlan, error) {
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
		decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.ID, scope, startedNothing)
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
	decided, err := s.freshDestroy(ctx, name, operation.Executable, operation.Source, scope, false)
	if err != nil {
		return operationPlan{}, err
	}
	decided.replacedPhases = s.computePhases(frozen)
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
