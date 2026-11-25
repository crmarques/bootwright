package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// CurrentSelection resolves the invoking user's context when none is explicit.
// It returns the selected name and identity so a stale marker is caught.
type CurrentSelection func(context.Context) (string, error)

type Executable struct {
	Version, Commit string
	Modified        bool
}

// ApplyAdmission observes prerequisites outside pure planning, before claims.
// It must not reopen lifecycle services while holding its snapshot.
type ApplyAdmission func(context.Context, string, api.Catalog, map[string]ObjectOwnership, []reconciliation.Block) error

// ExecutionContract identifies the current request, evidence, persisted formats and effect pins.
type ExecutionContract struct {
	ContentDigest string `json:"contentDigest"`
	Fingerprint   string `json:"fingerprint"`
}

type Options struct {
	DriveMachine      func(context.Context, Execution, BlockEvidence, bool) error
	PasswordMatches   func([]byte, []byte) bool
	Contracts         func(reconciliation.Block) (ExecutionContract, bool)
	Preparations      []PreparationDefinition
	BeginInvocation   BeginInvocation
	BeginPrompts      BeginInvocation
	Confirmer         Confirmer
	Presenter         PlanPresenter
	Progress          ProgressReporter
	Clock             Clock
	Entropy           reconciliation.Entropy
	Selection         CurrentSelection
	ApplyAdmission    ApplyAdmission
	ListenerAdmission ListenerAdmission
	BlockAdmission    func(context.Context, reconciliation.Plan, reconciliation.Block) error
	PlanInspection    PlanInspection
	Readiness         Readiness
	EffectBearing     EffectBearing
	HostKeys          ControllerHostKeys
	// Concurrency bounds running blocks without entering the plan; zero takes the default.
	Concurrency int
	Throttle    int
	Executable  Executable
	Operations  OperationStoreFactory
	// Setup and Lock locate the controller runtime every launch runs, and
	// Rebuild republishes it when only its directory is missing.
	Setup   SetupReader
	Lock    RuntimeLock
	Rebuild RuntimeRebuilder
	Paths   ContextPaths
}

type Service struct {
	keepGoing  bool
	startCheck func(View) error
	// Only the installation phase following this invocation's completed preparation inherits admission.
	completedPreparation string
	explicitPreparation  *reconciliation.PreparationScope
	previewPreparation   bool
	workspace            Workspace
	compiler             Compiler
	binder               SecretBinder
	host                 HostIdentity
	automation           AutomationIdentity
	guard                ExecutionGuard
	capabilities         CapabilityResolver
	options              Options
	relaxing             eraseRelaxing
}

func New(workspace Workspace, compiler Compiler, binder SecretBinder, host HostIdentity, automation AutomationIdentity, guard ExecutionGuard, capabilities CapabilityResolver, options Options) Service {
	return Service{workspace: workspace, compiler: compiler, binder: binder, host: host, automation: automation, guard: guard, capabilities: capabilities, options: options}
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.configured() {
		return availability.ErrNotImplemented
	}
	return nil
}

func (s Service) configured() bool {
	if s.workspace == nil || s.compiler == nil || s.binder == nil || s.host == nil || s.automation == nil || s.guard == nil || s.capabilities == nil {
		return false
	}
	return s.options.Clock != nil && s.options.Entropy != nil && s.options.Operations != nil && s.options.EffectBearing != nil
}

// resolve fixes the context this invocation acts on. An omitted name uses the
// invoking user's selection and refuses a stale marker.
func (s Service) resolve(ctx context.Context, name string) (string, error) {
	return resolveContext(ctx, s.options.Selection, name)
}

func resolveContext(ctx context.Context, selection CurrentSelection, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	if selection == nil {
		return "", engineWiringMissing.Fail("current context selection is not configured", "")
	}
	selected, err := selection(ctx)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", failure("context.state", "no current context is selected",
			"select one with bootwright context use --name <context>, or create one with bootwright context init --name <context>")
	}
	return selected, nil
}

func (s Service) store(view View) OperationStore {
	return s.options.Operations(view.Operations(), view.Logs())
}

func (s Service) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
	s, err := s.withParallelism(request.Parallelism)
	if err != nil {
		return nil, err
	}
	s.keepGoing = request.KeepGoing && request.Verb == reconciliation.Destroy
	result, err := s.relaxingFor(relaxingPlan, request.Relaxations).planWithin(ctx, request, nil)
	if s.keepGoing && result != nil && err != nil {
		result.Checks = append(result.Checks, PlanCheck{
			ID: "destroy-admission", Status: "failed", Summary: "No cleanup would start under the current checks",
		})
	}
	return result, err
}

func (s Service) planWithin(ctx context.Context, request PlanRequest, scope *reconciliation.PreparationScope) (*PlanResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	selection, err := reconciliation.ParseSelection(request.Stages, request.Until)
	if err == nil {
		selection, err = selection.WithTargets(request.Clusters, request.Machines)
	}
	if err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	if scope == nil && request.Verb != reconciliation.Destroy {
		if scope, _, err = s.automaticPreparation(ctx, name, selection, request.Readiness || request.Verb == ""); err != nil {
			return nil, err
		}
	}
	var result *PlanResult
	var decided operationPlan
	var read basis
	decision := s.decider(scope)
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		var err error
		result, decided, read, err = s.preview(ctx, decision, view, request.Verb, selection)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := s.refuseLostBinding(ctx, name, decided, read); err != nil {
		return result, err
	}
	inspectApply := s.options.PlanInspection != nil && decided.verb == reconciliation.Apply
	if result.Finalizes || result.Settled || decided.unclaimed || (!request.Readiness && !inspectApply) {
		return result, nil
	}

	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		// Read the same snapshot again before making an observation. A concurrent
		// invocation can move the context while the binding is inspected.
		if view.Identity() != result.Context || decided.basis.input != "" && inputDigest(view) != decided.basis.input {
			return engineInputMoved.Fail("the input changed before preview checks", "repeat the dry run for this context")
		}
		refuse := func(latest basis) error { return s.contextChanged(decided.verb, read, latest) }
		if _, _, _, err := s.verifyBasis(ctx, s.store(view), read, refuse); err != nil {
			return err
		}
		if request.Readiness {
			if err := s.previewReadiness(ctx, view, decided, read, result); err != nil {
				return err
			}
		}
		if inspectApply {
			state, err := decision.sources.compile(ctx, view)
			if err != nil {
				return err
			}
			checks, err := s.options.PlanInspection(ctx, name, state.Effective(), decided.plan, decided.starting())
			result.Checks = append(result.Checks, checks...)
			return err
		}
		return nil
	})
	return result, err
}

func (s Service) previewReadiness(ctx context.Context, view View, decided operationPlan, read basis, result *PlanResult) error {
	if err := s.verifyRemovalHost(ctx, view); err != nil {
		return err
	}
	if _, err := s.locateRuntime(ctx, false); err != nil {
		return err
	}
	check, err := s.prepareStart(ctx, view, decided, read)
	if err != nil {
		return err
	}
	if check != nil {
		if err := check(view); err != nil {
			return err
		}
		result.Checks = append(result.Checks, PlanCheck{ID: "initial-power", Status: "passed", Summary: "Managed Machines are stopped or absent"})
	}
	if decided.verb == reconciliation.Destroy {
		if s.keepGoing {
			if err := survivorRefusal(decided.plan.Blocks, decided.phases.survivors); err != nil {
				return err
			}
		}
		result.Checks = append(result.Checks, PlanCheck{ID: "removal-evidence", Status: "deferred", Summary: "Removal proofs run against recorded ownership before effects"})
	}
	return nil
}

// preview takes the decision of the verb it previews, through the path that
// verb takes, so it refuses exactly where that verb refuses and otherwise shows
// the plan the verb then presents. Where a finalization is due it decides as
// the verb does once that finalization is done, without performing it. It
// stops at the decision: it allocates no identity and writes nothing. It also
// returns that decision and the durable state it was read from, before any
// finalization it previews.
func (s Service) preview(ctx context.Context, decision decider, view View, previewed reconciliation.Verb, selection reconciliation.Selection) (*PlanResult, operationPlan, basis, error) {
	if previewed == "" && len(decision.relaxing.methods) != 0 {
		previewed = reconciliation.Destroy
	}
	decided, verb, err := decision.decideAs(ctx, view, previewed, selection)
	if err != nil {
		return nil, operationPlan{}, basis{}, err
	}
	decideSelection := selection
	if verb == reconciliation.Destroy {
		decideSelection = reconciliation.Selection{}
	}
	marked, finalizes := decided, decided.finalize
	if finalizes {
		if decided, err = decision.afterFinalization(ctx, view, verb, decideSelection, decided); err != nil {
			return nil, operationPlan{}, basis{}, err
		}
	}
	if verb == reconciliation.Destroy && !selection.Empty() {
		given := []string{}
		if selection.Targeted() {
			given = append(given, "--clusters or --machines")
		}
		if len(selection.Stages) != 0 {
			given = append(given, "--stage")
		}
		if selection.Until != "" {
			given = append(given, "--until")
		}
		return nil, operationPlan{}, basis{}, destroyStageSelected.Fail(
			"destroy --dry-run accepts no stage selection",
			"repeat "+contextCommand(view.Identity().Name, "destroy --dry-run")+" without "+strings.Join(given, " and "))
	}
	if err := s.refuseInapplicableRelaxations(view.Identity().Name, decided); err != nil {
		return nil, operationPlan{}, basis{}, err
	}
	guardedFinalization := false
	if finalizes && s.startsFlow(decided) {
		blocks, err := s.startBlocks(ctx, view, decided)
		if err != nil {
			return nil, operationPlan{}, basis{}, err
		}
		guardedFinalization = len(blocks) != 0
	}
	var result PlanResult
	switch {
	case finalizes && decided.noop || guardedFinalization:
		result = planPreview(marked.plan, marked.states, reconciliation.Selection{}, s.Throttle())
		result.Verb, result.Continuation, result.Finalizes = string(verb), true, true
		result.Receipt = Receipt{Operation: marked.operation.ID, Verb: "plan", State: "preview", Next: string(verb)}
		if !guardedFinalization {
			result.Receipt.Next = nextAction(marked.operation, marked.plan, marked.states)
		}
	case decided.noop:
		result = planPreview(reconciliation.Plan{}, nil, reconciliation.Selection{}, s.Throttle())
		result.Verb, result.Settled = string(verb), true
		operation := decided.operation.ID
		if operation == "" {
			operation = "none"
		}
		result.Receipt = Receipt{Operation: operation, Verb: "plan", State: "preview", Next: "none"}
	case decided.unclaimed:
		result = planPreview(reconciliation.Plan{}, nil, reconciliation.Selection{}, s.Throttle())
		result.Verb = string(verb)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: string(verb)}
	case decided.fresh:
		result = s.presentation(decided)
		result.Receipt = Receipt{Operation: "none", Verb: "plan", State: "preview", Next: string(decided.verb)}
	default:
		result = s.presentation(decided)
		result.Receipt = Receipt{Operation: decided.operation.ID, Verb: "plan", State: "preview", Next: nextAction(decided.operation, decided.plan, decided.states)}
	}
	result.Context, result.Bound = view.Identity(), s.Concurrency()
	if result.KeepGoing {
		store := s.store(view)
		if err := previousRemovalFailures(ctx, store, decided.basis, result.Steps); err != nil {
			return &result, decided, marked.basis, err
		}
		if err := sourceProofPreview(ctx, store, decided.basis, result.Steps, abandonedBlocks(decided.plan)); err != nil {
			return &result, decided, marked.basis, err
		}
	}
	return &result, decided, marked.basis, nil
}

func steps(plan reconciliation.Plan, states map[string]reconciliation.BlockState) []PlanStep {
	schedule := reconciliation.ScheduleOf(plan, 0)
	position := make(map[string]int, len(plan.Blocks))
	for index, block := range plan.Blocks {
		position[block.ID] = index + 1
	}
	out := make([]PlanStep, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		state := string(reconciliation.BlockPending)
		if states != nil {
			if current, ok := states[block.ID]; ok && current != "" {
				state = string(current)
			}
		}
		var after []int
		for _, dependency := range block.Dependencies {
			if place, known := position[dependency]; known {
				after = append(after, place)
			}
		}
		slices.Sort(after)
		step := PlanStep{
			ID: block.ID, Description: block.Description, Implementation: block.Implementation, Kind: block.Kind, Object: block.Object, Stage: string(block.Stage),
			Impacts: slices.Clone(block.Impacts), State: state,
			After: after, Wave: schedule.Waves[block.ID] + 1, Consumes: slices.Clone(block.Consumes),
		}
		if block.Inverse != nil {
			step.Retains, step.LeavesWith, step.Nothing = slices.Clone(block.Inverse.Retains), slices.Clone(block.Inverse.LeavesWith), block.Inverse.Nothing
		}
		out = append(out, step)
	}
	return out
}

// planPreview marks what a stage selection would start and why it would leave
// the rest, so an operator reads the consequence of the selection before
// confirming it. The marks are what the next apply's admission takes first:
// an unproved block it resolves holds back every other ready block, which is
// deferred behind the first of them, while the failed blocks it retries start
// beside every ready block that does not wait on one of them. Without a
// selection the steps carry no marker.
func planPreview(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection, throttle int) PlanResult {
	schedule := reconciliation.ScheduleOf(plan, throttle)
	result := PlanResult{
		Steps: steps(plan, states), Stages: selection.Stages.Names(), Until: selection.Until,
		Clusters: slices.Clone(selection.Clusters), Machines: slices.Clone(selection.Machines),
		Waves: schedule.Count, Widest: schedule.Widest,
	}
	authorized := authorizationPlan(plan, states, selection)
	for index, block := range plan.Blocks {
		if len(block.Consumes) != 0 {
			if _, included := authorized.Block(block.ID); included {
				result.AuthorizationSteps = append(result.AuthorizationSteps, index+1)
			}
		}
	}
	for _, token := range requiredTokens(authorized) {
		result.Authorizations = append(result.Authorizations, token+"="+AuthorizationDigest(authorized, token))
	}
	if selection.Empty() {
		return result
	}
	effects, blockers := reachableEffects(plan, states, selection)
	schedule = reconciliation.ScheduleOf(effectSchedule(plan, effects), throttle)
	result.Waves, result.Widest = schedule.Count, schedule.Widest
	kind, admitted := firstAdmission(plan, states, selection, nil, nil, nil)
	holding := ""
	if kind != StepStart {
		holding = heldBehind(plan, states, admitted)
	}
	deferrals := reconciliation.Deferrals(plan, states, selection)
	for index, step := range result.Steps {
		block := plan.Blocks[index]
		if step.State == string(reconciliation.BlockDone) {
			continue
		}
		if slices.Contains(admitted, index) && !(block.Stage == reconciliation.StageController && !selection.Stages.Selects(block.Stage)) {
			result.Steps[index].Selection = kind
			if kind == StepRetry && step.State != string(reconciliation.BlockFailed) {
				result.Steps[index].Selection = StepStart
			}
			result.Startable++
			continue
		}
		deferral := deferrals[step.ID]
		result.Deferred++
		switch {
		case !selection.Admits(block):
			result.Steps[index].Selection = StepNotSelected
			result.Excluded++
		case !effects[step.ID]:
			result.Steps[index].Selection, result.Steps[index].WaitsOn = StepBlocked, blockers[step.ID]
			result.Blocked++
		default:
			result.Steps[index].Selection = StepWaiting
			result.Steps[index].WaitsOn = deferral.Block
			if deferral.Block == "" {
				result.Steps[index].WaitsOn = holding
			}
			result.Later++
		}
	}
	return result
}

// heldBehind names the block a ready block waits on while admission resolves,
// the first block it admits, or, where a selection admits no failed block, the
// first failed block, which the apply refuses over.
func heldBehind(plan reconciliation.Plan, states map[string]reconciliation.BlockState, admitted []int) string {
	if len(admitted) != 0 {
		return plan.Blocks[admitted[0]].ID
	}
	for _, block := range plan.Blocks {
		if states[block.ID] == reconciliation.BlockFailed {
			return block.ID
		}
	}
	return ""
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

func previousRemovalFailures(ctx context.Context, store OperationStore, from basis, steps []PlanStep) error {
	if from.record.Verb != reconciliation.Destroy {
		return nil
	}
	for index := range steps {
		step := &steps[index]
		if from.blocks[step.ID] != reconciliation.BlockFailed || from.attempts[step.ID] == 0 {
			continue
		}
		attempt, err := store.Attempt(ctx, from.operation, step.ID, from.attempts[step.ID])
		if err != nil {
			return err
		}
		step.PreviousFailure = &BlockFailure{Message: "The previous attempt failed"}
		if cause := attempt.Failure; cause != nil {
			*step.PreviousFailure = BlockFailure{Code: cause.Code, Message: cause.Message, Remedy: cause.Remediation}
		}
	}
	return nil
}

func sourceProofPreview(ctx context.Context, store OperationStore, from basis, steps []PlanStep, abandoned map[string]bool) error {
	var states map[string]reconciliation.BlockState
	switch {
	case from.record.Verb == reconciliation.Apply:
		states = from.blocks
	case len(from.record.DeferredSourceProofs) != 0:
		member, err := readDeferredSource(ctx, store, from.record)
		if err != nil {
			return err
		}
		states = member.states
	}
	markSourceProofs(steps, states, abandoned)
	return nil
}

func markSourceProofs(steps []PlanStep, states map[string]reconciliation.BlockState, abandoned map[string]bool) {
	for index := range steps {
		if unproved(states[steps[index].ID]) && !abandoned[steps[index].ID] {
			steps[index].Selection = StepResolve
		}
	}
}
