package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strconv"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// recordingContext is the boundary an outcome is recorded under. Every
// operation-store write refuses a cancelled context, so an interrupt that
// recorded nothing would leave the block durably running: unproved, so no
// later operation continues past, removes or deletes it until an observation
// resolves it, although the attempt had proved its outcome. What the attempt
// proved is written whether or not the invocation that performed it was
// interrupted.
func recordingContext(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

// attempt runs one block: it allocates and records the attempt before the
// first side effect, executes it inside the controller's private runtime, then
// records the durable outcome its evidence justifies. A required-log failure
// is the boundary's to report, so it is never this block's cause.
//
// Until its running record is durable it has performed nothing, so an attempt
// that cannot start returns no state and the block keeps the one it had: a
// pending block stays pending and a failed one it would have retried stays
// failed, exactly as their records still say.
func (s Service) attempt(ctx context.Context, tx Transaction, store OperationStore, approved bundle, boundary *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block, material map[string]secrets.Material, position, total int) (reconciliation.BlockState, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return "", failure("lifecycle.state",
			"this executable does not offer the implementation this block froze",
			"install the executable that registered this operation")
	}
	// What the attempt relies on is read before it starts, so a record that
	// cannot be read leaves the block exactly as it was.
	proved, err := provedDependencies(ctx, store, operation, plan, block)
	if err != nil {
		return "", err
	}
	number, err := store.StartAttempt(ctx, operation.ID, block.ID)
	if err != nil {
		return "", err
	}
	logPath, err := operationstore.AttemptLogPath(operation.ID, block.ID, number, 0)
	if err != nil {
		return reconciliation.BlockPending, err
	}
	recording := recordingContext(ctx)
	log, err := boundary.open(ctx, logPath)
	if err != nil {
		// The effect never ran, but an attempt proves no absence: the
		// required-log failure leaves it unknown until an observation.
		return reconciliation.BlockUnknown, store.CompleteAttempt(recording, operation.ID, block.ID, number,
			reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, nil)
	}
	defer boundary.close(ctx, log)
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: "running", Position: position, Total: total})
	result, runErr := s.invoke(ctx, tx, store, approved, boundary, operation, plan, block, material, proved, log, number, 0, position, total, func(inner context.Context, execution Execution) (Result, error) {
		if operation.Verb == reconciliation.Destroy {
			if err := s.keepBeforeRemoval(inner, tx, boundary, log, capability, execution); err != nil {
				return Result{Outcome: reconciliation.OutcomeFailed}, err
			}
			return capability.Destroy(inner, execution)
		}
		return capability.Apply(inner, execution)
	})
	runErr = stageReading(runErr, block, operation.Verb, tx.Identity().Name)
	outcome := result.Outcome
	if !reconciliation.ValidOutcome(outcome) {
		outcome = reconciliation.OutcomeUnknown
	}
	if runErr != nil && errors.Is(runErr, context.Canceled) {
		outcome = reconciliation.OutcomeCanceled
	}
	// What the attempt produced is in custody before its record says done.
	outcome, runErr = s.capturedAttempt(ctx, tx, boundary, log, operation.Verb, block, outcome, result.Produced, runErr)
	_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "outcome", Block: block.ID, Detail: string(outcome)})
	// A typed failure proves less than completion, so an attempt whose log
	// failed before its outcome was logged records it unknown.
	if log.Failed() && outcome == reconciliation.OutcomeFailed {
		outcome = reconciliation.OutcomeUnknown
	}
	effect, state, err := reconciliation.AttemptTransition(outcome)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	if err := store.CompleteAttempt(recording, operation.ID, block.ID, number, outcome, effect, state, result.Evidence); err != nil {
		return reconciliation.BlockUnknown, err
	}
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: string(state), Position: position, Total: total})
	return state, runErr
}

// resolveUnknown observes the exact frozen request read-only under a freshly
// allocated resolution identity and log, before any observation begins. Until
// both exist it has observed nothing, so it returns no state and the block
// keeps the one it had: a resolution that cannot start moves nothing. It
// observes for the verb the operation froze: a destroy's block through
// ObserveRemoval, so a target its removal has not yet taken back is no
// removal, and an apply's block through Observe. Where removal says a fresh
// removal resolves an apply's block, an Observe that proves it completed
// resolves it done and captures what it produced, as the apply's own
// finalization does (D123); otherwise that removal reads the apply's block by
// its own check, through the block of removalPlan that takes it back, which
// keeps the apply's identity, implementation, content digest and request
// (D119), and the resolution is still recorded against the apply's block, as
// removalEffect maps it. A block it leaves unknown names command, the exact
// command that observes it again.
func (s Service) resolveUnknown(ctx context.Context, tx Transaction, store OperationStore, approved bundle, boundary *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block, material map[string]secrets.Material, position, total int, removal bool, removalPlan reconciliation.Plan, command string) (reconciliation.BlockState, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return "", failure("lifecycle.state",
			"this executable cannot observe the implementation this block froze",
			"install the executable that registered this operation")
	}
	observed, verb, err := observedAs(tx.Identity().Name, operation, block, removal, removalPlan)
	if err != nil {
		return "", err
	}
	attemptNumber, err := store.LastAttempt(ctx, operation.ID, block.ID)
	if err != nil {
		return "", err
	}
	number, err := store.StartResolution(ctx, operation.ID, block.ID, attemptNumber)
	if err != nil {
		return "", err
	}
	logPath, err := operationstore.AttemptLogPath(operation.ID, block.ID, attemptNumber, number)
	if err != nil {
		return "", err
	}
	recording := recordingContext(ctx)
	log, err := boundary.open(ctx, logPath)
	if err != nil {
		return "", nil
	}
	defer boundary.close(ctx, log)
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Detail: "resolving the unknown outcome from live evidence", Status: "running", Position: position, Total: total})
	read := func(target reconciliation.Block, as reconciliation.Verb) (Observation, error) {
		var observation Observation
		_, runErr := s.invoke(ctx, tx, store, approved, boundary, operation, plan, target, material, nil, log, attemptNumber, number, position, total, func(inner context.Context, execution Execution) (Result, error) {
			observe := capability.Observe
			if as == reconciliation.Destroy {
				observe = capability.ObserveRemoval
			}
			value, err := observe(inner, execution)
			observation = value
			return Result{Outcome: reconciliation.OutcomeUnknown}, err
		})
		return observation, stageReading(runErr, target, as, tx.Identity().Name)
	}
	observation, byRemoval, runErr := resolutionReading(ctx, boundary, log, operation.Verb, block, observed, verb, removal, read)
	resolvedEffect, state, err := reconciliation.ResolutionTransition(observedEffect(observation, runErr, byRemoval))
	if err != nil {
		ClearProduced(observation.Produced)
		return reconciliation.BlockUnknown, err
	}
	// A log failure here still permits the transition positive evidence
	// proves; the fault it latches is what blocks the work that would follow.
	for _, reported := range diagnostics.Of(runErr) {
		_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "observation-failed", Block: block.ID, Detail: reported.Code + ": " + reported.Message})
	}
	recorded := observationFailure(runErr)
	// A resolution that proves an apply's block done places what it produced
	// in custody first; one that cannot leaves the block unknown. Its failure
	// is custody's, never the observation's, so it records no failure.
	captured := s.capture(ctx, tx, boundary, log, operation.Verb, block, state, observation.Produced)
	if captured != nil {
		resolvedEffect, state = reconciliation.EffectUnknown, reconciliation.BlockUnknown
	}
	_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "resolution", Block: block.ID, Detail: string(resolvedEffect)})
	outcome := reconciliation.ResolutionOutcome(resolvedEffect, observation.Outcome)
	if err := store.CompleteResolution(recording, operation.ID, block.ID, attemptNumber, number, outcome, resolvedEffect, state, observation.Evidence, recorded); err != nil {
		return reconciliation.BlockUnknown, err
	}
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: string(state), Position: position, Total: total})
	switch {
	case state == reconciliation.BlockDone:
		return state, nil
	case captured != nil:
		return state, captured
	// An observation that never ran carries the reason it could not, and that
	// reason is actionable where the unresolved diagnosis is not: it names a
	// target the operator can restore rather than one already reachable. An
	// undiagnosed failure says nothing more than the unresolved diagnosis.
	case runErr != nil && (recorded != nil || ctx.Err() != nil):
		return state, runErr
	// The removal's check proved the block this context's own or absent, and
	// the removal takes it back next, so the apply's remedies do not apply.
	case removal && state == reconciliation.BlockFailed:
		return state, nil
	case resolvedEffect == reconciliation.EffectNoEffect:
		return state, failure("lifecycle.state",
			"the frozen effect was never performed",
			resolutionRemedy(tx.Identity().Name, operation.Verb, plan, false))
	case resolvedEffect == reconciliation.EffectPartial:
		return state, failure("lifecycle.state",
			"the frozen effect is partly realized and owned by this context",
			resolutionRemedy(tx.Identity().Name, operation.Verb, plan, true))
	}
	explanation := s.explain(verb, observed, observation.Evidence)
	_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "unresolved", Block: block.ID, Detail: explanation.Reason})
	return state, unresolvedFailure(block.ID, explanation, command)
}

// resolutionReading is the observation a resolution resolves block by, and
// whether that is the removal's own check of observed, read for verb. A fresh
// removal first reads the apply's block by the apply's own observation (D123):
// one that proves it completed resolves it done, so what it produced is
// captured before any inverse runs. Anything less, which the log records, is
// resolved by the removal's own check (D119). Every other resolution reads
// observed once, for verb.
func resolutionReading(ctx context.Context, boundary *logBoundary, log *operationstore.Log, applied reconciliation.Verb, block, observed reconciliation.Block, verb reconciliation.Verb, removal bool, read func(reconciliation.Block, reconciliation.Verb) (Observation, error)) (Observation, bool, error) {
	if !removal {
		observation, err := read(observed, verb)
		return observation, false, err
	}
	observation, err := read(block, applied)
	if err == nil && observation.Effect == reconciliation.EffectCompleted {
		return observation, false, nil
	}
	ClearProduced(observation.Produced)
	for _, reported := range diagnostics.Of(err) {
		_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "observation-failed", Block: block.ID, Detail: reported.Code + ": " + reported.Message})
	}
	_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "apply-observation", Block: block.ID, Detail: string(observedEffect(observation, err, false))})
	if ctx.Err() != nil {
		return Observation{}, true, err
	}
	observation, err = read(observed, verb)
	return observation, true, err
}

// observedAs is the block a resolution reads and the verb it reads it for: the
// block itself, for the verb its operation froze, or, where a fresh removal
// resolves an apply's block, the removal block that takes it back, read for
// the removal.
func observedAs(contextName string, operation operationstore.Operation, block reconciliation.Block, removal bool, removalPlan reconciliation.Plan) (reconciliation.Block, reconciliation.Verb, error) {
	if !removal {
		return block, operation.Verb, nil
	}
	taken, found := removalPlan.Block(block.ID)
	if !found {
		return reconciliation.Block{}, "", failure("lifecycle.state",
			"this removal carries no block that takes back "+block.ID, reviewStatus(contextName))
	}
	return taken, reconciliation.Destroy, nil
}

// observedEffect is the effect an observation proves: unknown when it failed
// or reported an effect outside the vocabulary, and, where a fresh removal
// resolves an apply's block, what that removal's check proves of the apply.
func observedEffect(observation Observation, runErr error, removal bool) reconciliation.EffectState {
	effect := observation.Effect
	if !reconciliation.ValidEffectState(effect) || runErr != nil {
		effect = reconciliation.EffectUnknown
	}
	if removal {
		effect = removalEffect(effect)
	}
	return effect
}

// removalEffect is what a removal's own check of an apply's block proves of
// that apply's effect. An absent target is the apply's positive absence of
// effect; a target that still shows everything the removal takes back, or
// part of it, as this context's own, is a positive partial realization. Both
// leave the block failed for the removal to take back. The check proves
// ownership and removability, never that the apply's frozen request was
// realized, so it never proves the block done; one that proves nothing leaves
// it unknown.
func removalEffect(observed reconciliation.EffectState) reconciliation.EffectState {
	switch observed {
	case reconciliation.EffectCompleted:
		return reconciliation.EffectNoEffect
	case reconciliation.EffectNoEffect, reconciliation.EffectPartial:
		return reconciliation.EffectPartial
	}
	return reconciliation.EffectUnknown
}

// invoke runs the capability inside the private Python execution boundary,
// exactly as controller setup does. The approved bundle is the operation's
// own, opened once before its first effect.
func (s Service) invoke(ctx context.Context, tx Transaction, store OperationStore, approved bundle, boundary *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, block reconciliation.Block, material map[string]secrets.Material, proved []BlockEvidence, log *operationstore.Log, attempt, resolution, position, total int, call func(context.Context, Execution) (Result, error)) (Result, error) {
	view := tx.Controller()
	result := Result{Outcome: reconciliation.OutcomeUnknown}
	// Completion is counted in proved groups, not in elapsed time: the adapter
	// reports each group once it settles and the frozen block declares them all.
	settled := map[string]struct{}{}
	target, err := operationstore.AdapterOutputPath(log.Path())
	if err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	// Retention is not a precondition of the run: what the adapter prints is
	// recorded beside this attempt whatever it proves, and never changes it.
	output := store.OpenAdapterOutput(ctx, target)
	defer func() {
		_ = output.Close(ctx)
		if bytes, truncated := output.Retained(); bytes > 0 {
			detail := path.Base(target) + ", " + strconv.Itoa(bytes) + " bytes"
			if truncated {
				detail += ", truncated"
			}
			_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "adapter-output", Block: block.ID, Detail: detail})
		}
	}()
	err = s.guard.WithPython(ctx, approved.area, approved.requirement, func(launch prerequisites.PythonLaunch, release func() error) error {
		// Only a block the plan froze into the controller stage receives that
		// stage's publication boundary. The stage is declared domain
		// vocabulary, so no implementation identity is read here.
		var stage *ControllerStage
		if block.Stage == reconciliation.StageController {
			stage = &ControllerStage{
				Setup:              view,
				ClientArea:         tx.ClientArea,
				SealClientArea:     tx.SealClientArea,
				RetainDependencies: tx.RetainDependencies,
				ReleaseFoundation:  release,
				Prepare: func(inner context.Context, preparation prerequisites.NativePreparation) error {
					if resolution != 0 {
						return failure("lifecycle.state", "an observation may not authorize a host effect", "")
					}
					encoded, err := json.Marshal(preparation)
					if err != nil {
						return failure("lifecycle.state", "the attempt before-state cannot be canonically represented", "")
					}
					return store.RecordPreparation(inner, operation.ID, block.ID, attempt, encoded)
				},
			}
		}
		// Only an apply's own attempt names the command that continues it: a
		// destroy's next command depends on what the whole run settles.
		continuation := ""
		if operation.Verb == reconciliation.Apply && resolution == 0 {
			continuation = contextCommand(tx.Identity().Name, string(reconciliation.Apply), authorizing(requiredTokens(plan))...)
		}
		execution := Execution{
			Operation: operation.ID, Context: tx.Identity().Name, Continuation: continuation, Attempt: attempt, Resolution: resolution, Block: block,
			Launch: launch, Bundle: approved.location, Area: approved.area, Material: material, Proved: proved,
			LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
				return s.locateTool(inner, store, view, tx.Identity().Name, operation, plan, tool)
			},
			Stage: stage,
			// A failed record latches the boundary, which cancels this run.
			Log: func(inner context.Context, record operationstore.LogRecord) error {
				return boundary.append(inner, log, record)
			},
			Output: output,
			Progress: func(inner context.Context, group, status string) {
				// Only a group the frozen block declares may advance completion,
				// because the declaration is what the count is measured against.
				if status != "running" && declaresGroup(block, group) {
					settled[group] = struct{}{}
				}
				s.report(inner, ProgressEvent{Block: block.ID, Description: block.Description, Group: group,
					Detail: groupDescription(block, group), Status: status, Position: position, Total: total,
					Completed: len(settled), Declared: len(block.Groups)})
			},
		}
		value, callErr := call(ctx, execution)
		result = value
		return callErr
	})
	return result, err
}

// stageReading names the controller stage command that settles a failure the
// foundation raised on entry, before the stage capability ran, for the calls
// the capability itself reads so: an apply's Apply and Observe. A removal's
// failure keeps its own remedy, because an incomplete destroy refuses every
// apply until it is continued.
func stageReading(err error, block reconciliation.Block, verb reconciliation.Verb, contextName string) error {
	if verb != reconciliation.Apply || block.Stage != reconciliation.StageController {
		return err
	}
	return prerequisites.InStage(err, contextName)
}

// locateTool asks the controller stage block this plan froze where the closure
// it proved published one executable. The stage is the one block whose clients
// every other block's adapter runs, so its capability alone answers; a plan
// without one has installed nothing to run. The proof is the one that block
// recorded in the apply whose effects this operation runs over, this apply or
// the one a removal takes back, because the stage's removal retains and
// proves no closure, and recovering the closure again from the host's shared
// sources would move a latest client to a release another context retained.
func (s Service) locateTool(ctx context.Context, store OperationStore, view prerequisites.StorageView, contextName string, operation operationstore.Operation, plan reconciliation.Plan, tool controller.InstalledTool) (string, error) {
	index := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool { return block.Stage == reconciliation.StageController })
	if index < 0 {
		return "", failure("controller.state",
			"the "+tool.Executable+" of release "+tool.Version+" is not installed on this controller",
			"run "+contextCommand(contextName, string(reconciliation.Apply), "--stage", string(reconciliation.StageController)))
	}
	stage := plan.Blocks[index]
	capability, ok := s.capabilities.Resolve(stage.Kind, stage.Implementation)
	locator, locates := capability.(ToolLocator)
	if !ok || !locates {
		return "", failure("lifecycle.state",
			"this executable cannot locate the clients the controller stage of this operation installed",
			"install the executable that registered this operation")
	}
	applied := operation.ID
	if operation.Verb == reconciliation.Destroy {
		applied = operation.Source
	}
	proved, err := blockEvidence(ctx, store, applied, stage, reconciliation.Apply)
	if err != nil {
		return "", err
	}
	return locator.LocateTool(ctx, view, stage, proved, tool)
}

func (s Service) report(ctx context.Context, event ProgressEvent) {
	if s.options.Progress != nil {
		s.options.Progress.ReportProgress(ctx, event)
	}
}

// groupDescription reads the frozen block's own description of a group, so
// the adapter reports only the stable identity and the plan supplies the prose.
func groupDescription(block reconciliation.Block, id string) string {
	for _, group := range block.Groups {
		if group.ID == id {
			return group.Description
		}
	}
	return id
}

func declaresGroup(block reconciliation.Block, id string) bool {
	return slices.ContainsFunc(block.Groups, func(group reconciliation.Group) bool { return group.ID == id })
}

// project publishes the context mutation evidence the operation state implies.
// The operation record is authoritative; the evidence is its projection.
func (s Service) project(ctx context.Context, tx Transaction, verb reconciliation.Verb, state reconciliation.OperationState) error {
	data, err := projection(verb, state)
	if err != nil {
		return err
	}
	return tx.PublishEvidence(ctx, data)
}

// finish records the operation's terminal state, withdraws the produced
// material and releases the reservations a completed removal no longer owns,
// and assembles the result the CLI renders. A
// log fault this invocation latched is recorded again here, so the operation
// keeps it even when the latch could not write it. A completed removal
// publishes no evidence here: its pristine evidence follows the release of its
// Secret bindings, outside this transaction, so evidence that is not yet
// pristine marks a removal whose finalization did not complete. An operation
// that did not complete also names the exact command its records call for.
func (s Service) finish(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, boundary, faulted bool, result *OperationResult) (*OperationResult, error) {
	next, err := reconciliation.NextOperationState(orderedStates(plan, states), boundary)
	if err != nil {
		return result, err
	}
	current, err := store.ReadOperation(ctx, operation.ID)
	if err != nil {
		return result, err
	}
	current.State, current.LogFault = next, current.LogFault || faulted
	if err := store.UpdateOperation(ctx, current); err != nil {
		return result, err
	}
	if next == reconciliation.OperationDone && operation.Verb == reconciliation.Destroy {
		if err := s.withdraw(ctx, tx); err != nil {
			return result, err
		}
		if err := tx.ReleaseReservations(ctx); err != nil {
			return result, err
		}
	} else if err := s.project(ctx, tx, operation.Verb, next); err != nil {
		return result, err
	}
	result.Blocks = blockResults(plan, states)
	logs, err := store.LogPaths(ctx, operation.ID, plan)
	if err == nil {
		result.Logs = logs
	}
	result.Receipt = Receipt{Operation: operation.ID, Verb: string(operation.Verb), State: string(next), Next: nextAction(current, plan, states)}
	if next == reconciliation.OperationDone {
		return result, nil
	}
	result.NextCommand = continuationCommand(tx.Identity().Name, current, plan, states)
	if next == reconciliation.OperationPaused {
		return result, nil
	}
	return result, terminalFailure(tx.Identity().Name, current, plan, states)
}

// terminalFailure reports what an operation that did not complete asks for, as
// the exact command its records call for. An unknown apply names the removal
// beside the resolution, because the removal proves the same effects itself
// and is the road out when the repair is to the automation the operation
// froze. A failed removal is replaced rather than continued, and says so.
func terminalFailure(contextName string, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) error {
	command := continuationCommand(contextName, operation, plan, states)
	switch {
	case operation.State == reconciliation.OperationUnknown && operation.Verb == reconciliation.Apply:
		return failure("lifecycle.unknown", "an effect has an unresolved outcome",
			"resolve it with "+command+", which observes that effect before anything else starts, or take back what it started with "+
				contextCommand(contextName, string(reconciliation.Destroy)))
	case operation.State == reconciliation.OperationUnknown:
		return failure("lifecycle.unknown", "an effect has an unresolved outcome", "resolve it with "+command)
	case nextAction(operation, plan, states) == string(reconciliation.Destroy):
		return failure("lifecycle.state", "the destroy did not complete",
			"repeat "+command+", which replaces it with a fresh removal of what it has not proved gone")
	}
	return failure("lifecycle.state", "the "+string(operation.Verb)+" did not complete", "continue it with "+command)
}

// nextAction names what an operation's own records call for, which is nothing
// once it completed. A finished apply admits a later destroy, but naming it
// here would read as an instruction to tear down what just succeeded; the
// verbs a context admits are what `plan` is for. A failed removal holding a
// block not done, an unknown one among them, is replaced by a fresh removal
// rather than continued or resolved, as the destroy decides; one whose blocks
// are all done is finalized, which completes that same removal. In any other
// incomplete operation a block that reads unknown is resolved before anything
// else starts, whatever state the operation records.
func nextAction(operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) string {
	switch {
	case operation.State == reconciliation.OperationDone:
		return "none"
	case operation.Verb == reconciliation.Destroy && operation.State == reconciliation.OperationFailed && pendingRemains(frozen, states):
		return string(reconciliation.Destroy)
	case slices.ContainsFunc(frozen.Blocks, func(block reconciliation.Block) bool { return states[block.ID] == reconciliation.BlockUnknown }):
		return "resolve"
	}
	return "continue-" + string(operation.Verb)
}

func blockResults(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []BlockResult {
	out := make([]BlockResult, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		out = append(out, BlockResult{ID: block.ID, Description: block.Description, Stage: string(block.Stage), State: string(state)})
	}
	return out
}

func logFault(err error) error {
	if err == nil {
		return nil
	}
	return failure("runtime.log",
		"a required private operation log could not be maintained",
		"restore the context store's private log tree before repeating the operation")
}

func digestOf(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
