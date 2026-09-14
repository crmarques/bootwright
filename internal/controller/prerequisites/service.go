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
}

func New(storage Storage, compiler Compiler, host HostInspector, catalog DependencyCatalog, bundle BundleManager, runtime RuntimeInstaller, options Options) Service {
	return Service{storage, compiler, host, catalog, bundle, runtime, options}
}

type inspection struct {
	view          StorageView
	selection     controller.Selection
	definition    Definition
	host          controller.InstalledHostIdentity
	platform      Platform
	bundle        BundleInspection
	runtime       RuntimeInspection
	bound         bool
	reusable      bool
	report        Report
	toolRequests  []controller.ToolRequest
	toolsResolved bool
	// tools, toolsPresent, libvirtClient and libvirtPresent describe what one
	// selected context adds to a ready host. They are evidence for preflight;
	// setup never selects, plans or installs them.
	tools          []ToolDefinition
	toolsPresent   bool
	libvirtClient  bool
	libvirtPresent bool
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.storage == nil || s.compiler == nil || s.host == nil || s.catalog == nil || s.bundle == nil {
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
				result = &current.report
			}
			return err
		}
		result = &current.report
		if current.contextReady() {
			result.Outcome = "ready"
			return nil
		}
		result.Outcome = "not-ready"
		return failure("preflight.failed", "required controller prerequisites are not ready", readinessCommand(*result))
	})
	return result, err
}

func (s Service) Setup(ctx context.Context, request SetupRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
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
	// A completed receipt can outlive its executable's automation revision.
	// Its validated definition remains historical evidence; only a fresh setup
	// may resolve a new compatible bundle. Pending retry and corruption refuse.
	if err != nil && (request.DryRun || s.options.Bootstrap == nil || current.view.State.Receipt.Status != "complete" || !errors.Is(err, ErrBootstrapIncompatible)) {
		return nil, err
	}
	if request.DryRun {
		current.report.Outcome = "planned"
		return &current.report, nil
	}
	if err == nil && current.ready() {
		current.report.Outcome = "unchanged"
		return &current.report, nil
	}
	if s.options.Bootstrap != nil && !(current.view.State.Receipt.ID != "" && current.view.State.Receipt.Incomplete()) && current.resolutionRequired() {
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
	approved := current
	err = s.storage.MutateController(ctx, current.view.Context, true, func(tx StorageTransaction) error {
		frozen := inspectionResolution{Sources: approved.definition.Sources}
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
		return s.prepare(ctx, tx, &current)
	})
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
}

// inspect verifies the host. A non-empty phase streams the scope and each
// check as it settles; the repeated inspections that bind a resolution and
// guard the transaction pass no phase, so every check is shown exactly once.
func (s Service) inspect(ctx context.Context, view StorageView, dryRun bool, phase string, frozen ...inspectionResolution) (inspection, error) {
	current := inspection{view: view, selection: controller.Baseline(), bundle: BundleInspection{Recoverable: true}, toolsResolved: true}
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
	current.libvirtClient = current.selection.LibvirtClient()
	// Setup owns the context-independent native closure only. A libvirt client
	// is selected by one context's desired state, so its controller stage
	// installs it and this inspection only reports whether it is present.
	requirements := NativeRequirements{ContainerRuntime: current.selection.ContainerRuntime()}
	if s.options.Bootstrap != nil {
		current.definition, current.toolsResolved, err = s.selectedResolution(current, requirements, frozen)
	} else {
		current.definition, err = s.catalog.Select(platform, requirements)
	}
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
			return current, err
		}
	}
	if err := s.catalog.ValidateEgress(current.route()); err != nil {
		return current, err
	}
	current.report = Report{ContextName: view.Context.Name, Machine: current.selection.MachineName(), Platform: platform, DryRun: dryRun, Outcome: "planned", Checks: []Check{}, Actions: []string{}}
	if s.options.Bootstrap != nil && !current.toolsResolved {
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
	if current.libvirtClient {
		current.report.Checks = append(current.report.Checks, Check{"libvirt-client", "libvirt client", "unverified", "unverified", ContextScope})
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
	start("installed-host", "verifying local identity")
	current.host, err = s.host.Identity(ctx)
	if err != nil {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return current, err
	}
	if view.State.Host.Valid() && !view.State.Host.Equal(current.host) {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return current, failure("controller.identity", "stored setup belongs to a different installed host", "restore the original host and state; setup cannot rebind it")
	}
	settle(readiness("installed-host", "verified local identity", true, HostScope))
	if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
		if !current.compatibleReceipt(view.State.Receipt) || !current.matchesActions(view.State.Receipt.Actions) {
			return current, failure("controller.unknown", "another exact setup attempt remains unresolved", "restore its original context, executable and acquisition route, then repeat controller setup")
		}
	}
	if view.OpenBundle != nil {
		start("execution-bundle", "verifying the retained bundle")
		area, err := view.OpenBundle(ctx, current.definition.CatalogDigest)
		if err != nil {
			settle(unverified("execution-bundle", current.versions(), HostScope))
			return current, err
		}
		if area != nil {
			current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
			if err != nil {
				settle(unverified("execution-bundle", current.versions(), HostScope))
				return current, err
			}
			current.reusable = current.definition.Bootstrap != nil
		}
	}
	settle(readiness("execution-bundle", current.versions(), current.bundle.Ready, HostScope))
	if !current.bundle.Ready {
		current.report.Actions = append(current.report.Actions, "Prepare and verify the pinned execution bundle")
	}
	if current.selection.ContainerRuntime() {
		start("container-runtime", "inspecting native packages")
		current.runtime, err = s.inspectRuntime(ctx, current.definition)
		if err != nil {
			settle(unverified("container-runtime", current.definition.Runtime.Version, HostScope))
			return current, err
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
	}
	if err := s.settleContextChecks(ctx, view, &current, settle, start); err != nil {
		return current, err
	}
	if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
		settle(Check{"setup-recovery", "complete", "incomplete", "not-ready", HostScope})
		current.report.Actions = append(current.report.Actions, "Resolve the exact pending setup receipt")
	} else if view.State.Receipt.ID == "" || view.State.Receipt.Status != "complete" || !current.bundle.Sealed {
		observed := view.State.Receipt.Status
		if observed == "" {
			observed = "missing"
		} else if observed == "complete" && !current.bundle.Sealed {
			observed = "selected bundle is not sealed"
		}
		settle(Check{"setup-state", "complete", observed, "not-ready", HostScope})
		current.report.Actions = append(current.report.Actions, "Record verified setup completion")
	}
	return current, nil
}

// settleContextChecks reports what one selected context still needs on this
// host. Every observation is read-only presence: the tools its desired state
// selects, its libvirt client closure, and its binding to this host. None of
// them is a setup action, because the context's controller stage owns them.
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
	if current.libvirtClient {
		start("libvirt-client", "inspecting the libvirt client packages")
		present, err := s.libvirtPresent(ctx, view)
		if err != nil {
			settle(unverified("libvirt-client", "libvirt client", ContextScope))
			return err
		}
		settle(readiness("libvirt-client", "libvirt client", present, ContextScope))
		current.libvirtPresent = present
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

// libvirtPresent proves the context's libvirt roots from a retained resolution
// that selected them. Without such a resolution nothing installed them, so the
// answer is a definite absence rather than an unverifiable check.
func (s Service) libvirtPresent(ctx context.Context, view StorageView) (bool, error) {
	if s.options.NativeInspector == nil {
		return false, nil
	}
	for index := len(view.State.RetainedDefinitions) - 1; index >= 0; index-- {
		definition := view.State.RetainedDefinitions[index]
		if !definition.NativeRequirements.LibvirtClient || definition.Native == nil {
			continue
		}
		presence, err := s.options.NativeInspector.Check(ctx, *definition.Native)
		if err != nil {
			return false, err
		}
		if presence.Ready {
			return true, nil
		}
	}
	return false, nil
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
// includes a context's own tools, libvirt client or binding.
func (i inspection) ready() bool {
	return i.bundle.Ready && i.bundle.Sealed && i.dependenciesReady() && i.view.State.Receipt.ID != "" && i.view.State.Receipt.Status == "complete"
}

// contextReady adds what one selected context needs on a ready host. Preflight
// requires it; setup neither observes nor prepares it.
func (i inspection) contextReady() bool {
	return i.ready() && i.bound && (len(i.toolRequests) == 0 || i.toolsPresent) && (!i.libvirtClient || i.libvirtPresent)
}

func (i inspection) dependenciesReady() bool {
	return !i.selection.ContainerRuntime() || i.runtime.Ready
}

// resolutionRequired reports whether publisher metadata must be consulted. A
// reusable retained resolution installs from its frozen closure, except that a
// missing native root needs a new transaction bound to the host's current
// package inventory.
func (i inspection) resolutionRequired() bool {
	return !i.reusable || i.selection.ContainerRuntime() && !i.runtime.Ready
}

func (i inspection) canPrepare() error {
	if i.bundle.Sealed && (!i.bundle.Ready || !i.dependenciesReady()) {
		return failure("controller.unknown", "a sealed dependency bundle or its prerequisites no longer match the approved closure", "restore the exact retained dependencies; setup cannot repair a sealed bundle")
	}
	if i.runtime.Conflict && i.definition.Native == nil {
		return failure("controller.unsupported", "the installed container runtime does not match the qualified closure", "prepare the qualified runtime without replacing dependencies through Bootwright")
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

func (i inspection) compatibleReceipt(receipt SetupReceipt) bool {
	return receipt.Context == i.view.Context && receipt.CatalogDigest == i.definition.CatalogDigest && slices.Equal(receipt.Sources, i.definition.Sources) && receipt.Egress.HTTPProxy == i.route().HTTPProxy && receipt.Egress.HTTPSProxy == i.route().HTTPSProxy && slices.Equal(receipt.Egress.NoProxy, i.route().NoProxy)
}

func (i inspection) samePlan(other inspection) bool {
	return i.host.Equal(other.host) && i.view.Context == other.view.Context && i.definition.CatalogDigest == other.definition.CatalogDigest && slices.Equal(i.definition.Sources, other.definition.Sources) && i.bundle == other.bundle && i.runtime == other.runtime && i.bound == other.bound && i.view.State.Receipt.ID == other.view.State.Receipt.ID && i.view.State.Receipt.Status == other.view.State.Receipt.Status && i.route().HTTPProxy == other.route().HTTPProxy && i.route().HTTPSProxy == other.route().HTTPSProxy && slices.Equal(i.route().NoProxy, other.route().NoProxy)
}

func cloneReport(report Report) Report {
	report.Checks = slices.Clone(report.Checks)
	report.Actions = slices.Clone(report.Actions)
	report.Dependencies = slices.Clone(report.Dependencies)
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

func setupCommand() string { return "run bootwright setup" }

func stageCommand(name string) string {
	return "run bootwright apply --stage controller --context " + name
}

// readinessCommand names the one command that settles what preflight found
// missing: setup for a host prerequisite, and the context's own controller
// stage for anything its desired state selects.
func readinessCommand(report Report) string {
	if PendingScope(report) == ContextScope && report.ContextName != "" {
		return stageCommand(report.ContextName)
	}
	return setupCommand()
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
