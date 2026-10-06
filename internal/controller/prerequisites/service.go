package prerequisites

import (
	"context"
	"errors"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Options struct {
	// AmbientRoute is the acquisition route the invoking environment selected.
	// It applies to context-free work alone; a selected context always uses its
	// own controller Machine's proxy choice.
	AmbientRoute    controller.Route
	Confirmer       Confirmer
	Presenter       PlanPresenter
	Progress        ProgressReporter
	Tools           TargetToolCatalog
	Bootstrap       BootstrapResolver
	Native          NativeResolver
	NativeInspector NativeInspector
}

type Service struct {
	storage  Storage
	compiler Compiler
	host     HostInspector
	catalog  DependencyCatalog
	bundle   BundleManager
	runtime  RuntimeInstaller
	options  Options
	// abandon is set on the copy one setup --purge-old-bundles runs as. That
	// setup cancels a receipt stranded at the bound which this executable
	// cannot resume, so each of its inspections judges the host as it will
	// be once that receipt is canceled.
	abandon bool
}

func New(storage Storage, compiler Compiler, host HostInspector, catalog DependencyCatalog, bundle BundleManager, runtime RuntimeInstaller, options Options) Service {
	return Service{storage: storage, compiler: compiler, host: host, catalog: catalog, bundle: bundle, runtime: runtime, options: options}
}

type inspection struct {
	view       StorageView
	selection  controller.Selection
	definition Definition
	host       controller.InstalledHostIdentity
	platform   Platform
	bundle     BundleInspection
	runtime    RuntimeInspection
	bound      bool
	reusable   bool
	// carried marks a resolution this executable rebased from a closure the
	// host already holds, and retainedDigest names the bundle those
	// bytes come from. Its publisher identities never changed, so it is frozen
	// exactly like a reusable one even though its own area is still empty.
	carried        bool
	retainedDigest string
	// abandoned is the stored receipt this setup cancels before it sets the
	// host up afresh, still pending as the store holds it, or none.
	abandoned     SetupReceipt
	report        Report
	toolRequests  []controller.ToolRequest
	toolsResolved bool
	// tools, toolsPresent, native, closures and admission describe what one
	// selected context adds to a ready host: its target tools, the native
	// closures its controller stage selects, their presence exactly as that
	// stage reads it, and the refusal of a selection this platform cannot
	// realize. They are evidence for preflight; setup never selects, plans or
	// installs them.
	tools        []ToolDefinition
	toolsPresent bool
	native       StageNative
	closures     []ClosurePresence
	admission    error
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Setup resolves every dependency it prepares, so a composition without
	// its resolution ports has no setup to offer and fails closed.
	if s.storage == nil || s.compiler == nil || s.host == nil || s.catalog == nil || s.bundle == nil ||
		s.options.Bootstrap == nil || s.options.Native == nil || s.options.NativeInspector == nil {
		return availability.ErrNotImplemented
	}
	return nil
}

func (s Service) Check(ctx context.Context, request CheckRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	var result *Report
	err := s.storage.ReadController(ctx, request.ContextName, func(view StorageView) error {
		current, err := s.inspect(ctx, view, false, ReadinessPhase)
		if err != nil {
			if len(current.report.Checks) != 0 {
				current.report.Outcome = "not-ready"
				current.report.Next = current.next()
				result = &current.report
			}
			return err
		}
		result = &current.report
		// A selection this platform cannot realize is settled by no command:
		// its own refusal and remedy are the answer, and no next command is
		// offered.
		if current.admission != nil {
			result.Outcome = "not-ready"
			return current.admission
		}
		if current.contextReady() {
			result.Outcome = "ready"
			return nil
		}
		result.Outcome = "not-ready"
		result.Next = current.next()
		return failure("preflight.failed", "required controller prerequisites are not ready", current.remedy())
	})
	return result, err
}

func (s Service) Setup(ctx context.Context, request SetupRequest) (*Report, error) {
	report, err := s.setup(ctx, request)
	// Retirement reads what the completed setup left behind, so it runs only
	// after one completed: a refusal, a failure and a preview retire nothing
	// here, because what may be retired is decided by what the bundle this
	// setup sealed now holds. The one earlier retirement is a host at its bound,
	// which gives up its superseded bundles before it can publish a new one.
	if err != nil || report == nil || !request.PurgeOldBundles {
		return report, err
	}
	if report.Outcome != "changed" && report.Outcome != "unchanged" {
		return report, nil
	}
	if err := s.retireSuperseded(ctx, report); err != nil {
		return report, err
	}
	return report, nil
}

// retireSuperseded removes every execution bundle a retained resolution names
// that the completed receipt does not, and completes every retirement an
// interruption left unfinished. A client area is never one of them, and
// neither is an area this record does not account for. A superseded bundle
// that holds no area, as a canceled receipt's does once a later receipt
// replaced it, has only its resolution left to retire. The report names the
// areas removed and nothing else.
func (s Service) retireSuperseded(ctx context.Context, report *Report) error {
	return s.storage.MutateController(ctx, SetupContext{}, false, func(tx StorageTransaction) error {
		view := tx.Snapshot()
		receipt := view.State.Receipt
		if receipt.Status != "complete" {
			return nil
		}
		var bundles, resolutions []string
		name := func(id string) {
			if id != "" && id != receipt.CatalogDigest && !slices.Contains(bundles, id) {
				bundles = append(bundles, id)
			}
		}
		for _, definition := range view.State.RetainedDefinitions {
			id := definition.CatalogDigest
			if slices.ContainsFunc(view.Areas, func(held HeldArea) bool { return held.ID == id }) {
				name(id)
			} else if id != "" && id != receipt.CatalogDigest && definition.ResolutionDigest != "" && setupResolution(definition) {
				resolutions = append(resolutions, definition.ResolutionDigest)
			}
		}
		// A retirement drops the resolutions its areas carry when it records
		// its intent, so after an interruption only the mark still names them.
		for _, held := range view.Areas {
			if held.Retiring {
				name(held.ID)
			}
		}
		slices.Sort(bundles)
		slices.Sort(resolutions)
		if len(bundles) != 0 {
			if err := tx.RetireBundles(ctx, bundles); err != nil {
				return err
			}
			report.RetiredBundles = append(report.RetiredBundles, bundles...)
			slices.Sort(report.RetiredBundles)
		}
		if len(resolutions) != 0 {
			return tx.RetireResolutions(ctx, resolutions)
		}
		return nil
	})
}

// setupResolution reports a resolution of the closure setup owns, which
// selects no native client and no target tool a context adds. A controller
// stage retains the others, and only that stage judges which are superseded.
func setupResolution(definition Definition) bool {
	requirements := definition.NativeRequirements
	return !requirements.LibvirtClient && !requirements.Hypervisor && !requirements.InstallerMedia && len(definition.Tools) == 0
}

func (s Service) setup(ctx context.Context, request SetupRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	s.abandon = request.PurgeOldBundles
	var current inspection
	var err error
	// Setup selects no context, so its dry run needs no stored evidence at all
	// and stays below the privilege boundary.
	if request.DryRun {
		current, err = s.inspect(ctx, StorageView{}, true, "")
	} else {
		err = s.storage.ReadController(ctx, "", func(view StorageView) error {
			current, err = s.inspect(ctx, view, false, InspectionPhase)
			return err
		})
	}
	// A settled receipt, whether its setup completed, failed or was canceled,
	// can outlive its executable's automation revision. Its validated
	// definition remains historical evidence; only a fresh setup may resolve a
	// new compatible bundle. Pending retry and corruption refuse.
	if err != nil && (request.DryRun || current.view.State.Receipt.Incomplete() || !errors.Is(err, ErrBootstrapIncompatible)) {
		return nil, err
	}
	if request.DryRun {
		current.report.Outcome = "planned"
		return &current.report, nil
	}
	// An executable whose embedded automation moved needs a new bundle, not new
	// dependencies. The retained closure is reprojected from the sources this
	// host already holds, so the releases, bytes and signers stay frozen. A
	// retained bundle that lost one of those sources is resolved afresh below,
	// exactly as an incompatible settled resolution is.
	if err != nil && errors.Is(err, ErrAutomationSuperseded) {
		current, err = s.carryForward(ctx, current)
		if err != nil && !errors.Is(err, ErrRetainedSourceUnavailable) {
			return &current.report, err
		}
	}
	if err == nil && current.ready() {
		current.report.Outcome = "unchanged"
		return &current.report, nil
	}
	if !(current.view.State.Receipt.ID != "" && current.view.State.Receipt.Incomplete()) && current.resolutionRequired() {
		current, err = s.resolveDependencies(ctx, current)
		if err != nil {
			return &current.report, err
		}
	}
	if current.ready() {
		current.report.Outcome = "unchanged"
		return &current.report, nil
	}
	if err := current.canPrepare(); err != nil {
		return &current.report, err
	}
	if !current.dependenciesReady() && s.runtime == nil {
		return &current.report, failure("controller.unsupported", "native runtime installation is not configured", "prepare the qualified runtime before repeating setup")
	}
	retiring, err := current.room(current.view, request.PurgeOldBundles)
	if err != nil {
		return &current.report, err
	}
	current.report.Actions = append(current.report.Actions, current.abandonment()...)
	current.report.Actions = append(current.report.Actions, retiring.actions()...)
	if s.options.Presenter == nil {
		return &current.report, failure("controller.setup", "setup plan presentation is not configured", "")
	}
	if err := s.options.Presenter.PresentControllerPlan(ctx, cloneReport(current.report)); err != nil {
		return nil, err
	}
	current.report.PlanPresented = true
	if !request.SkipConfirmation {
		if s.options.Confirmer == nil {
			return &current.report, failure("controller.setup", "setup requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, "setup", "this host"); err != nil {
			return &current.report, err
		}
	}
	if err := ctx.Err(); err != nil {
		return &current.report, err
	}
	host, err := s.host.Identity(ctx)
	if err != nil {
		return &current.report, err
	}
	if !current.host.Equal(host) {
		return &current.report, failure("controller.identity", "installed host changed after confirmation", "restore the original host before repeating setup")
	}
	return s.executeApprovedPlan(ctx, current, request)
}

func (s Service) executeApprovedPlan(ctx context.Context, approved inspection, request SetupRequest) (*Report, error) {
	current := approved
	var retired []string
	err := s.storage.MutateController(ctx, current.view.Context, true, func(tx StorageTransaction) error {
		var err error
		if retired, err = s.makeRoom(ctx, tx, approved, request.PurgeOldBundles); err != nil {
			return err
		}
		frozen := inspectionResolution{Sources: approved.definition.Sources, Retained: approved.retainedDigest}
		if approved.definition.Bootstrap != nil {
			frozen.Definition = &approved.definition
		}
		fresh, err := s.inspect(ctx, tx.Snapshot(), false, "", frozen)
		if err != nil {
			return err
		}
		if !approved.samePlan(fresh) {
			return failure("controller.conflict", "controller state changed after plan confirmation", setupCommand())
		}
		current = fresh
		current.report.PlanPresented = true
		current.report.ProgressPresented = approved.report.ProgressPresented
		current.report.Warnings = approved.report.Warnings
		return s.prepare(ctx, tx, &current)
	})
	current.report.RetiredBundles = retired
	if err != nil {
		// Only an attempt that reached its durable intent can be incomplete. A
		// refusal raised before that changed nothing and must not imply that
		// setup left effects behind.
		current.report.Outcome = "planned"
		if len(current.report.Progress) != 0 {
			current.report.Outcome = "incomplete"
		}
		return &current.report, err
	}
	current.report.Outcome = "changed"
	for index := range current.report.Checks {
		current.report.Checks[index].Status = "ready"
		current.report.Checks[index].Observed = current.report.Checks[index].Required
	}
	return &current.report, nil
}

type inspectionResolution struct {
	Sources    []DependencySource
	Definition *Definition
	// Retained names the bundle a carried definition was reprojected
	// from, so every later inspection of that plan keeps reading its sources
	// from the host instead of a publisher.
	Retained string
}

// inspect verifies the host. A non-empty phase streams the scope and each
// check as it settles; the repeated inspections that bind a resolution and
// guard the transaction pass no phase, so every check is shown exactly once.
// A receipt this setup abandons is inspected as already canceled.
func (s Service) inspect(ctx context.Context, view StorageView, dryRun bool, phase string, frozen ...inspectionResolution) (inspection, error) {
	abandoned := s.abandoned(ctx, view)
	if abandoned.ID != "" {
		view.State.Receipt = canceled(abandoned)
	}
	current, err := s.selectInspection(ctx, view, frozen)
	current.abandoned = abandoned
	if err != nil {
		return current, err
	}
	platform := current.platform
	current.report = Report{ContextName: view.Context.Name, Machine: current.selection.MachineName(), Platform: platform, DryRun: dryRun, Outcome: "planned", Route: current.selection.Route().Summary(), Checks: []Check{}, Actions: []string{}}
	if !current.toolsResolved {
		current.report.Dependencies = dependencyIntent(current.selection)
	}
	for _, source := range current.definition.Sources {
		origin, err := url.Parse(source.URL)
		if err != nil {
			return current, failure("controller.unsupported", "dependency catalog source is invalid", "use a compatible executable")
		}
		current.report.Dependencies = append(current.report.Dependencies, path.Base(origin.Path))
	}
	stream := phase != "" && !dryRun
	if stream && s.options.Presenter != nil {
		if err := s.options.Presenter.PresentControllerScope(ctx, phase, cloneReport(current.report)); err != nil {
			return current, err
		}
		current.report.ProgressPresented = true
	}
	// settle records a check's outcome and, while streaming, shows it the
	// moment it is known; start announces the verification about to run.
	settle := func(check Check) {
		current.report.setCheck(check)
		if stream {
			s.report(ctx, &current.report, checkEvent(phase, check))
		}
	}
	start := func(id, detail string) {
		if stream {
			s.report(ctx, &current.report, ProgressEvent{Phase: phase, Action: id, Status: "running", Detail: detail})
		}
	}
	hostText := platform.OS + " " + platform.Release + "/" + platform.Architecture
	settle(Check{"host", hostText, hostText, "ready", HostScope})
	current.report.Checks = append(current.report.Checks, Check{"installed-host", "verified local identity", "unverified", "unverified", HostScope}, Check{"execution-bundle", current.versions(), "unverified", "unverified", HostScope})
	if current.selection.ContainerRuntime() {
		current.report.Checks = append(current.report.Checks, Check{"container-runtime", current.definition.Runtime.Version, "unverified", "unverified", HostScope})
	}
	// A selected context adds checks its own controller stage settles. They are
	// reported, never planned here, because setup installs nothing a context
	// selects and never publishes a binding.
	if len(current.toolRequests) != 0 {
		current.report.Checks = append(current.report.Checks, Check{"target-tools", current.toolSummary(), "unverified", "unverified", ContextScope})
	}
	if current.native.LibvirtClient {
		current.report.Checks = append(current.report.Checks, Check{"libvirt-client", closureRequirement("libvirt-client"), "unverified", "unverified", ContextScope})
	}
	if current.native.Hypervisor {
		current.report.Checks = append(current.report.Checks, Check{"hypervisor", closureRequirement("hypervisor"), "unverified", "unverified", ContextScope})
	}
	if current.native.InstallerMedia {
		current.report.Checks = append(current.report.Checks, Check{"installer-media", closureRequirement("installer-media"), "unverified", "unverified", ContextScope})
	}
	if view.Context.Name != "" {
		current.report.Checks = append(current.report.Checks, Check{"controller-binding", current.selection.MachineName(), "unverified", "unverified", ContextScope})
	}
	if dryRun {
		current.report.Actions = append(current.report.Actions, "Resolve requested versions, verify the installed host and prepare the exact execution bundle")
		if current.selection.ContainerRuntime() {
			current.report.Actions = append(current.report.Actions, "Resolve and review native package changes, then install and verify them with Ansible")
		}
		return current, nil
	}
	if err := s.settleHostChecks(ctx, view, &current, settle, start); err != nil {
		return current, err
	}
	if current.selection.ContainerRuntime() {
		if err := s.settleRuntimeCheck(ctx, &current, settle, start); err != nil {
			return current, err
		}
	}
	if err := s.settleContextChecks(ctx, view, &current, settle, start); err != nil {
		return current, err
	}
	settleSetupState(view, &current, settle)
	return current, nil
}

func (s Service) selectInspection(ctx context.Context, view StorageView, frozen []inspectionResolution) (inspection, error) {
	current := inspection{view: view, selection: controller.Baseline().WithAmbientRoute(s.options.AmbientRoute), bundle: BundleInspection{Recoverable: true}, toolsResolved: true}
	if len(frozen) != 0 && frozen[0].Retained != "" {
		current.carried, current.retainedDigest = true, frozen[0].Retained
	}
	if view.Context.Name != "" {
		state, _, err := s.compiler.Compile(ctx, view.Sources)
		if err != nil {
			return current, err
		}
		if state == nil {
			return current, failure("context.input", "context input could not be admitted", "import Environment input with context update")
		}
		current.selection, err = controller.Select(state.Effective())
		if err != nil {
			return current, err
		}
		current.view.Context.Machine = current.selection.MachineName()
		current.toolRequests, err = controller.SelectTools(state.Effective())
		if err != nil {
			return current, err
		}
	}
	platform, err := s.host.Platform(ctx)
	if err != nil {
		return current, err
	}
	current.platform = platform
	current.native = StageNativeOf(current.selection)
	// Setup owns the context-independent native closure only. The libvirt
	// client, the hypervisor and the installer-media tooling are selected by
	// one context's desired state, so its controller stage installs them and
	// this inspection only reports whether they are present.
	requirements := NativeRequirements{ContainerRuntime: current.selection.ContainerRuntime()}
	current.definition, current.toolsResolved, err = s.selectedResolution(current, requirements, frozen)
	if err != nil {
		return current, err
	}
	current.definition.Sources = slices.Clone(current.definition.Sources)
	current.definition.Runtime.Files = slices.Clone(current.definition.Runtime.Files)
	current.definition.Runtime.Links = slices.Clone(current.definition.Runtime.Links)
	current.definition.Execution.Files = slices.Clone(current.definition.Execution.Files)
	current.definition.Execution.Links = slices.Clone(current.definition.Execution.Links)
	current.definition.Execution.Preload = slices.Clone(current.definition.Execution.Preload)
	// A context's target tools are recovered from retained identities only, so
	// this inspection reads no publisher metadata and acquires nothing.
	if len(current.toolRequests) != 0 {
		if s.options.Tools == nil {
			return current, failure("controller.unsupported", "target tool selection is not configured", "use a compatible executable")
		}
		current.tools, current.toolsResolved, err = s.options.Tools.Select(current.toolRequests, view.State.RetainedSources)
		if err != nil {
			// Target tools are selected only by a context, and only its
			// controller stage installs them.
			if view.Context.Name != "" {
				err = InStage(err, view.Context.Name)
			}
			return current, err
		}
	}
	if err := s.catalog.ValidateEgress(current.route()); err != nil {
		return current, err
	}
	return current, nil
}

func (s Service) settleHostChecks(ctx context.Context, view StorageView, current *inspection, settle func(Check), start func(string, string)) error {
	start("installed-host", "verifying local identity")
	var err error
	current.host, err = s.host.Identity(ctx)
	if err != nil {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return err
	}
	if view.State.Host.Valid() && !view.State.Host.Equal(current.host) {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return failure("controller.identity", "stored setup belongs to a different installed host", "restore the original host and state; setup cannot rebind it")
	}
	settle(readiness("installed-host", "verified local identity", true, HostScope))
	if receipt := view.State.Receipt; receipt.ID != "" && receipt.Incomplete() {
		own := current.compatibleReceipt(receipt) && current.matchesActions(receipt.Actions)
		foreign := !own || !current.sameRoute(receipt)
		// A stranded receipt resumes only after a retirement, which is never
		// undone, makes room for it, so this executable first proves that it
		// can prepare that receipt's bundle at all. One it cannot prepare is
		// abandoned instead when its setup never took effect, whatever route
		// it recorded.
		if own && stranded(view) {
			if err := s.bundle.Validate(current.definition); err != nil {
				if !errors.Is(err, ErrBootstrapIncompatible) {
					return err
				}
				if abandonable(view) {
					return unresumable()
				}
				foreign = true
			}
		}
		if foreign {
			return failure("controller.unknown", "another exact setup attempt remains unresolved", "restore the executable that recorded it and the HTTPS_PROXY, HTTP_PROXY and NO_PROXY values it ran with, then "+setupCommand())
		}
	}
	// Only a resolved definition identifies a bundle. An unresolved setup has
	// nothing retained to verify, and the store admits no other identity.
	if view.OpenBundle != nil && current.definition.CatalogDigest != "" {
		start("execution-bundle", "verifying the retained bundle")
		area, err := view.OpenBundle(ctx, current.definition.CatalogDigest)
		if err != nil {
			settle(unverified("execution-bundle", current.versions(), HostScope))
			return err
		}
		if area != nil {
			current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
			if err != nil {
				settle(unverified("execution-bundle", current.versions(), HostScope))
				return err
			}
			current.reusable = current.definition.Bootstrap != nil
		}
	}
	settle(readiness("execution-bundle", current.versions(), current.bundle.Ready, HostScope))
	if !current.bundle.Ready {
		current.report.Actions = append(current.report.Actions, "Prepare and verify the pinned execution bundle")
	}
	return nil
}

func (s Service) settleRuntimeCheck(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	start("container-runtime", "inspecting native packages")
	var err error
	current.runtime, err = s.inspectRuntime(ctx, current.definition)
	if err != nil {
		settle(unverified("container-runtime", current.definition.Runtime.Version, HostScope))
		return err
	}
	check := readiness("container-runtime", current.definition.Runtime.Version, current.runtime.Ready, HostScope)
	if current.runtime.Version != "" {
		check.Observed = current.runtime.Version
	}
	settle(check)
	if !current.runtime.Ready {
		current.report.Actions = append(current.report.Actions, "Run the Ansible controller role to install and verify missing native prerequisites")
	}
	if current.definition.Native != nil && !current.runtime.Ready {
		for _, action := range current.definition.Native.Actions {
			current.report.Actions = append(current.report.Actions, describeNativeAction(action))
		}
	}
	return nil
}

func settleSetupState(view StorageView, current *inspection, settle func(Check)) {
	if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
		settle(Check{"setup-recovery", "complete", "incomplete", "not-ready", HostScope})
		current.report.Actions = append(current.report.Actions, "Resolve the exact pending setup receipt")
	} else if view.State.Receipt.ID == "" || view.State.Receipt.Status != "complete" || !current.bundle.Sealed {
		observed := view.State.Receipt.Status
		// The receipt this setup abandons stays pending until it cancels it.
		if current.abandoned.ID != "" {
			observed = current.abandoned.Status
		}
		if observed == "" {
			observed = "missing"
		} else if observed == "complete" && !current.bundle.Sealed {
			observed = "selected bundle is not sealed"
		}
		settle(Check{"setup-state", "complete", observed, "not-ready", HostScope})
		current.report.Actions = append(current.report.Actions, "Record verified setup completion")
	}
}

// settleContextChecks reports what one selected context still needs on this
// host. Every observation is read-only presence: the tools its desired state
// selects, the native closures its controller stage selects, and its binding
// to this host. None of them is a setup action, because the context's
// controller stage owns them.
func (s Service) settleContextChecks(ctx context.Context, view StorageView, current *inspection, settle func(Check), start func(string, string)) error {
	if view.Context.Name == "" {
		current.bound = true
		return nil
	}
	if len(current.toolRequests) != 0 {
		start("target-tools", "verifying the retained target tools")
		ready := false
		if current.toolsResolved && view.OpenBundle != nil && s.options.Tools != nil {
			area, err := view.OpenBundle(ctx, ToolsDigest(current.tools))
			if err != nil {
				settle(unverified("target-tools", current.toolSummary(), ContextScope))
				return err
			}
			if area != nil {
				ready, err = s.options.Tools.Present(ctx, area, current.tools)
				if err != nil {
					settle(unverified("target-tools", current.toolSummary(), ContextScope))
					return err
				}
			}
		}
		settle(readiness("target-tools", current.toolSummary(), ready, ContextScope))
		current.toolsPresent = ready
	}
	if err := s.settleClosures(ctx, view, current, settle, start); err != nil {
		return err
	}
	for _, binding := range view.State.Bindings {
		if binding.Context != view.Context.Name {
			continue
		}
		digest, _ := current.host.PrivateDigest()
		if binding.Machine != current.selection.MachineName() || binding.HostDigest != digest {
			return failure("controller.identity", "selected controller does not match its established host binding", "restore the bound controller input; apply cannot rebind it")
		}
		current.bound = true
	}
	settle(readiness("controller-binding", current.selection.MachineName(), current.bound, ContextScope))
	return nil
}

// settleClosures reports each native closure the context's stage selects
// exactly as that stage reads it: from the latest resolution of its own
// selection, whose absence is a definite absence because nothing installed
// those roots, and on RHEL the installer-media tooling from what the operator
// installed. A selection this platform cannot realize settles its closures as
// unsupported and is kept as the inspection's refusal.
func (s Service) settleClosures(ctx context.Context, view StorageView, current *inspection, settle func(Check), start func(string, string)) error {
	if !current.native.selects() {
		return nil
	}
	current.admission = StageAdmission(current.platform, current.native)
	ids := closureIDs(current.native)
	start(ids[0], "inspecting the native client packages")
	closures, err := StageClosures(ctx, s.options.NativeInspector, view.State.RetainedDefinitions, current.platform, current.native)
	if err != nil {
		for _, id := range ids {
			settle(unverified(id, closureRequirement(id), ContextScope))
		}
		return InStage(err, view.Context.Name)
	}
	current.closures = closures
	for _, closure := range closures {
		check := readiness(closure.ID, closureRequirement(closure.ID), closure.Ready, ContextScope)
		if current.admission != nil && closure.ID != "installer-media" {
			check.Observed = "unsupported on " + current.platform.OS + " " + current.platform.Release
		}
		settle(check)
	}
	return nil
}

// closureIDs names the closures a selection names, in report order.
func closureIDs(native StageNative) []string {
	var ids []string
	for _, closure := range closureTable {
		if closure.selected(native) {
			ids = append(ids, closure.id)
		}
	}
	return ids
}

// closureRequirement is what a closure check requires, in the operator's words.
func closureRequirement(id string) string {
	switch id {
	case "hypervisor":
		return "hypervisor closure"
	case "installer-media":
		return "installer-media tooling (lorax, xorriso)"
	}
	return "libvirt client"
}

func (i inspection) versions() string {
	return "Python " + i.definition.PythonVersion + ", Ansible " + i.definition.AnsibleVersion
}

// toolSummary names the context clients by exact release once their identities
// are retained, and by requirement until then, so the check always states
// something the operator can act on.
func (i inspection) toolSummary() string {
	if summary := toolVersionSummary(i.tools); summary != "" {
		return summary
	}
	kinds := make([]string, 0, len(i.toolRequests))
	for _, request := range i.toolRequests {
		kinds = append(kinds, request.Kind)
	}
	return strings.Join(kinds, ", ")
}

// ready is host readiness: everything context-independent setup owns. It never
// includes a context's own tools, native closures or binding.
func (i inspection) ready() bool {
	return i.bundle.Ready && i.bundle.Sealed && i.dependenciesReady() && i.view.State.Receipt.ID != "" && i.view.State.Receipt.Status == "complete"
}

// contextReady adds what one selected context needs on a ready host. Preflight
// requires it; setup neither observes nor prepares it.
func (i inspection) contextReady() bool {
	return i.ready() && i.bound && (len(i.toolRequests) == 0 || i.toolsPresent) && i.closuresReady()
}

// closuresReady requires every native closure the context's stage selects,
// each settled present, on a platform that can realize the selection.
func (i inspection) closuresReady() bool {
	return i.admission == nil && len(i.closures) == len(closureIDs(i.native)) && ClosuresReady(i.closures)
}

// operatorPending reports that preflight found missing the installer-media
// tooling a RHEL controller's operator installs, which only that operator's
// own step, before the stage, settles. Tooling it could not inspect is not
// found missing: the failure that stopped the inspection names its remedy.
func (i inspection) operatorPending() bool {
	if !i.native.InstallerMedia || !OperatorInstallerMedia(i.platform) {
		return false
	}
	for _, closure := range i.closures {
		if closure.ID == "installer-media" {
			return !closure.Ready
		}
	}
	return false
}

// operatorStep is the operator's step for the installer-media tooling
// preflight found pending, as a correction and as a command.
func (i inspection) operatorStep() (correction, invocation string) {
	for _, closure := range i.closures {
		if closure.ID == "installer-media" {
			return installerMediaStep(closure.Foreign)
		}
	}
	return installerMediaStep(nil)
}

func (i inspection) dependenciesReady() bool {
	return !i.selection.ContainerRuntime() || i.runtime.Ready
}

// frozenBootstrap reports that this inspection already holds the exact Python
// and Ansible closure it will publish, either because its retained bundle
// serves it or because it was reprojected from one. No publisher can move it.
func (i inspection) frozenBootstrap() bool { return i.reusable || i.carried }

// resolutionRequired reports whether publisher metadata must be consulted. A
// frozen bootstrap installs from its own closure, except that a missing native
// root needs a new transaction bound to the host's current package inventory.
func (i inspection) resolutionRequired() bool {
	return !i.frozenBootstrap() || i.selection.ContainerRuntime() && !i.runtime.Ready
}

func (i inspection) canPrepare() error {
	if i.bundle.Sealed && (!i.bundle.Ready || !i.dependenciesReady()) {
		return failure("controller.unknown", "a sealed dependency bundle or its prerequisites no longer match the approved closure", "restore the exact retained dependencies; setup cannot repair a sealed bundle")
	}
	if i.view.State.Receipt.ID != "" && i.view.State.Receipt.Incomplete() {
		for _, action := range i.view.State.Receipt.Actions {
			if action.ID == "container-runtime" && action.Phase != "planned" && len(action.Preparation) != 0 && ((!i.runtime.Ready && i.definition.Native == nil) || !i.bundle.Recoverable) {
				return failure("controller.unknown", "the native runtime transaction has no verified terminal postcondition", "resolve the original native transaction and restore its exact prerequisites before repeating setup")
			}
		}
	}
	return nil
}

func (i inspection) route() SetupEgress {
	route := i.selection.Route()
	bypass := route.NoProxy()
	if bypass == nil {
		bypass = []string{}
	}
	return SetupEgress{HTTPProxy: route.HTTPProxy(), HTTPSProxy: route.HTTPSProxy(), NoProxy: bypass}
}

// compatibleReceipt compares a pending receipt with what setup, which selects
// no context, would record, so one naming a context is never setup's own.
func (i inspection) compatibleReceipt(receipt SetupReceipt) bool {
	return receipt.Context == (SetupContext{}) && receipt.CatalogDigest == i.definition.CatalogDigest && slices.Equal(receipt.Sources, i.definition.Sources)
}

// sameRoute compares the route a pending receipt of setup's recorded with the
// one this setup would acquire over. That route is setup's ambient one, which
// a context's inspection never reads, so only an inspection without a context
// compares it; setup enforces it again when it resumes.
func (i inspection) sameRoute(receipt SetupReceipt) bool {
	route := i.route()
	return i.view.Context.Name != "" || receipt.Egress.HTTPProxy == route.HTTPProxy && receipt.Egress.HTTPSProxy == route.HTTPSProxy && slices.Equal(receipt.Egress.NoProxy, route.NoProxy)
}

func (i inspection) samePlan(other inspection) bool {
	return i.host.Equal(other.host) && i.view.Context == other.view.Context && i.definition.CatalogDigest == other.definition.CatalogDigest && slices.Equal(i.definition.Sources, other.definition.Sources) && i.bundle == other.bundle && i.runtime == other.runtime && i.bound == other.bound && i.view.State.Receipt.ID == other.view.State.Receipt.ID && i.view.State.Receipt.Status == other.view.State.Receipt.Status && i.route().HTTPProxy == other.route().HTTPProxy && i.route().HTTPSProxy == other.route().HTTPSProxy && slices.Equal(i.route().NoProxy, other.route().NoProxy)
}

func cloneReport(report Report) Report {
	report.Checks = slices.Clone(report.Checks)
	report.Actions = slices.Clone(report.Actions)
	report.Dependencies = slices.Clone(report.Dependencies)
	report.Warnings = slices.Clone(report.Warnings)
	return report
}

// setCheck settles a check in place, or appends one that was not declared up
// front, so the report keeps catalog order either way.
func (r *Report) setCheck(check Check) {
	for index := range r.Checks {
		if r.Checks[index].ID == check.ID {
			r.Checks[index] = check
			return
		}
	}
	r.Checks = append(r.Checks, check)
}

// checkEvent is the progress row a settled check streams: its outcome in the
// progress vocabulary and its summary as the detail.
func checkEvent(phase string, check Check) ProgressEvent {
	status := "unknown"
	switch check.Status {
	case "ready":
		status = "ok"
	case "not-ready":
		status = "failed"
	}
	return ProgressEvent{Phase: phase, Action: check.ID, Status: status, Detail: check.Summary()}
}

// unverified settles a check whose verification itself failed, so the row
// closes before the diagnostic explains why.
func unverified(id, required, scope string) Check {
	return Check{id, required, "not verified", "not-ready", scope}
}

func readiness(id, required string, ready bool, scope string) Check {
	if ready {
		return Check{id, required, required, "ready", scope}
	}
	return Check{id, required, "missing or unverified", "not-ready", scope}
}

func setupCommand() string { return "run " + setupInvocation }

const setupInvocation = "bootwright setup"

func stageInvocation(name string) string {
	return "bootwright apply --stage controller --context " + name
}

func stageCommand(name string) string { return "run " + stageInvocation(name) }

// next names the one command that settles what preflight found missing:
// setup for a host prerequisite, and for what a context selects its own
// controller stage, after the operator's own installation of a RHEL
// controller's installer-media tooling when that is missing. A context with no
// stage has nothing pending but its binding, which its first apply publishes.
// A selection this platform cannot realize has no command at all.
func (i inspection) next() string {
	name := i.report.ContextName
	if i.admission != nil {
		return ""
	}
	if PendingScope(i.report) != ContextScope || name == "" {
		return setupInvocation
	}
	if i.operatorPending() {
		_, invocation := i.operatorStep()
		return invocation + ", then " + stageInvocation(name)
	}
	if controller.HasStage(i.selection, i.toolRequests) {
		return stageInvocation(name)
	}
	return "bootwright apply --context " + name
}

// remedy is the readiness refusal's remediation: the command next names, led
// by the operator's correction when the installer-media tooling of a RHEL
// controller is what is missing.
func (i inspection) remedy() string {
	if name := i.report.ContextName; name != "" && i.operatorPending() && PendingScope(i.report) == ContextScope {
		correction, _ := i.operatorStep()
		return correction + ", then " + stageCommand(name)
	}
	return "run " + i.next()
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
