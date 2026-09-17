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

// attempt runs one block: it allocates and records the attempt before the
// first side effect, executes it inside the controller's private runtime, then
// records the durable outcome its evidence justifies.
func (s Service) attempt(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, block reconciliation.Block, material map[string]secrets.Material, position, total int) (reconciliation.BlockState, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return reconciliation.BlockFailed, failure("lifecycle.state",
			"this executable does not offer the implementation this block froze",
			"install the executable that registered this operation")
	}
	number, err := store.StartAttempt(ctx, operation.ID, block.ID)
	if err != nil {
		return reconciliation.BlockPending, err
	}
	logPath, err := operationstore.AttemptLogPath(operation.ID, block.ID, number, 0)
	if err != nil {
		return reconciliation.BlockPending, err
	}
	log, err := store.OpenLog(ctx, logPath)
	if err != nil {
		return reconciliation.BlockUnknown, logFault(err)
	}
	defer func() { _ = log.Close(ctx) }()
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: "running", Position: position, Total: total})
	result, runErr := s.invoke(ctx, tx, store, operation, block, material, log, number, 0, position, total, func(inner context.Context, execution Execution) (Result, error) {
		if operation.Verb == reconciliation.Destroy {
			return capability.Destroy(inner, execution)
		}
		return capability.Apply(inner, execution)
	})
	outcome := result.Outcome
	if !reconciliation.ValidOutcome(outcome) {
		outcome = reconciliation.OutcomeUnknown
	}
	if runErr != nil && errors.Is(runErr, context.Canceled) {
		outcome = reconciliation.OutcomeCanceled
	}
	effect, state, err := reconciliation.AttemptTransition(outcome)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	_ = log.Append(ctx, operationstore.LogRecord{Event: "outcome", Block: block.ID, Detail: string(outcome)})
	if err := store.CompleteAttempt(ctx, operation.ID, block.ID, number, outcome, effect, state, result.Evidence); err != nil {
		return reconciliation.BlockUnknown, err
	}
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: string(state), Position: position, Total: total})
	return state, runErr
}

// resolveUnknown observes the exact frozen request read-only under a freshly
// allocated resolution identity and log, before any observation begins.
func (s Service) resolveUnknown(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, block reconciliation.Block, material map[string]secrets.Material, position, total int) (reconciliation.BlockState, error) {
	capability, ok := s.capabilities.Resolve(block.Kind, block.Implementation)
	if !ok {
		return reconciliation.BlockUnknown, failure("lifecycle.state",
			"this executable cannot observe the implementation this block froze",
			"install the executable that registered this operation")
	}
	attemptNumber, err := store.LastAttempt(ctx, operation.ID, block.ID)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	number, err := store.StartResolution(ctx, operation.ID, block.ID, attemptNumber)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	logPath, err := operationstore.AttemptLogPath(operation.ID, block.ID, attemptNumber, number)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	log, err := store.OpenLog(ctx, logPath)
	if err != nil {
		return reconciliation.BlockUnknown, logFault(err)
	}
	defer func() { _ = log.Close(ctx) }()
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Detail: "resolving the unknown outcome from live evidence", Status: "running", Position: position, Total: total})
	var observation Observation
	_, runErr := s.invoke(ctx, tx, store, operation, block, material, log, attemptNumber, number, position, total, func(inner context.Context, execution Execution) (Result, error) {
		value, err := capability.Observe(inner, execution)
		observation = value
		return Result{Outcome: reconciliation.OutcomeUnknown}, err
	})
	effect := observation.Effect
	if !reconciliation.ValidEffectState(effect) || runErr != nil {
		effect = reconciliation.EffectUnknown
	}
	resolvedEffect, state, _, err := reconciliation.ResolutionTransition(effect)
	if err != nil {
		return reconciliation.BlockUnknown, err
	}
	for _, reported := range diagnostics.Of(runErr) {
		_ = log.Append(ctx, operationstore.LogRecord{Event: "observation-failed", Block: block.ID, Detail: reported.Code + ": " + reported.Message})
	}
	_ = log.Append(ctx, operationstore.LogRecord{Event: "resolution", Block: block.ID, Detail: string(resolvedEffect)})
	if err := store.CompleteResolution(ctx, operation.ID, block.ID, attemptNumber, number, resolvedEffect, state, observation.Evidence); err != nil {
		return reconciliation.BlockUnknown, err
	}
	s.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: string(state), Position: position, Total: total})
	switch {
	case state == reconciliation.BlockDone:
		return state, nil
	// An observation that never ran carries the reason it could not, and that
	// reason is actionable where the unresolved diagnosis is not: it names a
	// target the operator can restore rather than one already reachable.
	case runErr != nil:
		return state, runErr
	case resolvedEffect == reconciliation.EffectNoEffect:
		return state, failure("lifecycle.state",
			"the frozen effect was never performed",
			"repeat the operation to perform it")
	case resolvedEffect == reconciliation.EffectPartial:
		return state, failure("lifecycle.state",
			"the frozen effect is partly realized and owned by this context",
			"repeat the operation to converge it, or destroy what it owns")
	}
	return state, failure("lifecycle.unknown",
		"the frozen effect could not be resolved from live evidence",
		"repeat the operation once the target is reachable, or restore the host it ran against")
}

// invoke opens the controller's approved bundle and runs the capability inside
// the private Python execution boundary, exactly as controller setup does.
func (s Service) invoke(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, block reconciliation.Block, material map[string]secrets.Material, log *operationstore.Log, attempt, resolution, position, total int, call func(context.Context, Execution) (Result, error)) (Result, error) {
	view := tx.Controller()
	area, location, requirement, err := approvedBundle(ctx, tx)
	if err != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, err
	}
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
			_ = log.Append(ctx, operationstore.LogRecord{Event: "adapter-output", Block: block.ID, Detail: detail})
		}
	}()
	err = s.guard.WithPython(ctx, area, requirement, func(launch prerequisites.PythonLaunch, release func() error) error {
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
		execution := Execution{
			Operation: operation.ID, Attempt: attempt, Resolution: resolution, Block: block, Launch: launch, Bundle: location, Area: area,
			Material: material,
			LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
				return prerequisites.LocateInstalledTool(inner, view, tool)
			},
			Stage: stage,
			Log: func(inner context.Context, record operationstore.LogRecord) error {
				return log.Append(inner, record)
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
	evidence, err := reconciliation.EvidenceFor(verb, state)
	if err != nil {
		return err
	}
	data, err := evidence.Bytes()
	if err != nil {
		return err
	}
	return tx.PublishEvidence(ctx, data)
}

// finish records the operation's terminal state, releases what a completed
// removal no longer owns, and assembles the result the CLI renders.
func (s Service) finish(ctx context.Context, tx Transaction, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, boundary bool, result *OperationResult) (*OperationResult, error) {
	ordered := make([]reconciliation.BlockState, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		ordered = append(ordered, state)
	}
	next, err := reconciliation.NextOperationState(ordered, boundary)
	if err != nil {
		return result, err
	}
	current, err := store.ReadOperation(ctx, operation.ID)
	if err != nil {
		return result, err
	}
	current.State = next
	if err := store.UpdateOperation(ctx, current); err != nil {
		return result, err
	}
	if next == reconciliation.OperationDone && operation.Verb == reconciliation.Destroy {
		if err := tx.ReleaseReservations(ctx); err != nil {
			return result, err
		}
	}
	if err := s.project(ctx, tx, operation.Verb, next); err != nil {
		return result, err
	}
	result.Blocks = blockResults(plan, states)
	logs, err := store.LogPaths(ctx, operation.ID, plan)
	if err == nil {
		result.Logs = logs
	}
	result.Receipt = Receipt{Operation: operation.ID, Verb: string(operation.Verb), State: string(next), Next: nextAction(operation.Verb, next)}
	if next == reconciliation.OperationDone || next == reconciliation.OperationPaused {
		return result, nil
	}
	return result, terminalFailure(next)
}

func terminalFailure(state reconciliation.OperationState) error {
	if state == reconciliation.OperationUnknown {
		return failure("lifecycle.unknown",
			"an effect has an unresolved outcome",
			"repeat the operation to resolve it from live evidence")
	}
	return failure("lifecycle.state",
		"the operation did not complete",
		"repeat the operation to continue it")
}

// nextAction names what an operation's own state calls for, which is nothing
// once it completed. A finished apply admits a later destroy, but naming it
// here would read as an instruction to tear down what just succeeded; the
// verbs a context admits are what `plan` is for.
func nextAction(verb reconciliation.Verb, state reconciliation.OperationState) string {
	switch state {
	case reconciliation.OperationDone:
		return "none"
	case reconciliation.OperationUnknown:
		return "resolve"
	}
	return "continue-" + string(verb)
}

// nextCommand is the command an operator actually runs for a next action. The
// action names a transition rather than a verb this executable offers: a
// continuation and a resolution are both reached by repeating the operation's
// own verb, and a completed one asks for nothing at all.
func nextCommand(verb reconciliation.Verb, action string) string {
	switch action {
	case "none":
		return ""
	case "resolve", "continue-apply", "continue-destroy":
		return "bootwright " + string(verb)
	}
	return "bootwright " + action
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
