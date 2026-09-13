package prerequisites

import (
	"context"
	"errors"
	"net/url"
	"path"
	"slices"

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
		current, err := s.inspect(ctx, view, false)
		if err != nil {
			if len(current.report.Checks) != 0 {
				current.report.Outcome = "not-ready"
				result = &current.report
			}
			return err
		}
		result = &current.report
		if current.ready() {
			result.Outcome = "ready"
			return nil
		}
		result.Outcome = "not-ready"
		return failure("preflight.failed", "required bastion prerequisites are not ready", setupCommand(request.ContextName))
	})
	return result, err
}

func (s Service) Setup(ctx context.Context, request SetupRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	var current inspection
	var err error
	if request.DryRun && request.ContextName == "" {
		current, err = s.inspect(ctx, StorageView{}, true)
	} else {
		err = s.storage.ReadController(ctx, request.ContextName, func(view StorageView) error {
			current, err = s.inspect(ctx, view, request.DryRun)
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
		current, err = s.resolveDependencies(ctx, request.ContextName, current)
		if err != nil {
			return &current.report, err
		}
	} else if !current.toolsResolved {
		// A pending receipt is replayed from its frozen resolution; it can never
		// discover the identities it failed to record.
		return &current.report, failure("controller.unknown", "pending setup lacks its exact target tool identities", "restore the original setup evidence")
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
		scope := request.ContextName
		if scope == "" {
			scope = "baseline"
		}
		if err := s.options.Confirmer.Confirm(ctx, "bastion setup", scope); err != nil {
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
		fresh, err := s.inspect(ctx, tx.Snapshot(), false, frozen)
		if err != nil {
			return err
		}
		if !approved.samePlan(fresh) {
			return failure("controller.conflict", "bastion state changed after plan confirmation", setupCommand(request.ContextName))
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

func (s Service) inspect(ctx context.Context, view StorageView, dryRun bool, frozen ...inspectionResolution) (inspection, error) {
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
	requirements := NativeRequirements{
		ContainerRuntime: current.selection.ContainerRuntime(),
		LibvirtClient:    current.selection.LibvirtClient(),
	}
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
	if len(current.toolRequests) != 0 && s.options.Bootstrap == nil {
		current.toolsResolved = false
		if s.options.Tools == nil {
			return current, failure("controller.unsupported", "target tool selection is not configured", "use a compatible executable")
		}
		retained := view.State.RetainedSources
		if len(frozen) != 0 {
			retained = frozen[0].Sources
		}
		if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
			retained = view.State.Receipt.Sources
		}
		tools, complete, err := s.options.Tools.Select(current.toolRequests, retained)
		if err != nil {
			return current, err
		}
		current.definition, err = WithTools(current.definition, tools)
		if err != nil {
			return current, err
		}
		current.toolsResolved = complete
	}
	if err := s.catalog.ValidateEgress(current.route()); err != nil {
		return current, err
	}
	current.report = Report{ContextName: view.Context.Name, Machine: current.selection.MachineName(), Platform: platform, DryRun: dryRun, Outcome: "planned", Checks: []Check{}, Actions: []string{}}
	if s.options.Bootstrap != nil && !current.toolsResolved {
		current.report.Dependencies = dependencyIntent(current.selection, current.toolRequests)
	}
	for _, source := range current.definition.Sources {
		origin, err := url.Parse(source.URL)
		if err != nil {
			return current, failure("controller.unsupported", "dependency catalog source is invalid", "use a compatible executable")
		}
		current.report.Dependencies = append(current.report.Dependencies, path.Base(origin.Path))
	}
	current.report.Checks = append(current.report.Checks, Check{"host", platform.OS + " " + platform.Release + "/" + platform.Architecture, platform.OS + " " + platform.Release + "/" + platform.Architecture, "ready"})
	current.report.Checks = append(current.report.Checks, Check{"installed-host", "verified local identity", "unverified", "unverified"}, Check{"execution-bundle", current.versions(), "unverified", "unverified"})
	if current.selection.ContainerRuntime() {
		current.report.Checks = append(current.report.Checks, Check{"container-runtime", current.definition.Runtime.Version, "unverified", "unverified"})
	}
	if len(current.toolRequests) != 0 {
		current.report.Checks = append(current.report.Checks, Check{"target-tools", "desired-state native clients", "unverified", "unverified"})
	}
	if view.Context.Name != "" {
		current.report.Checks = append(current.report.Checks, Check{"controller-binding", current.selection.MachineName(), "unverified", "unverified"})
	}
	if dryRun {
		current.report.Actions = append(current.report.Actions, "Resolve requested versions, verify the installed host and prepare the exact execution bundle")
		if current.selection.ContainerRuntime() {
			current.report.Actions = append(current.report.Actions, "Resolve and review native package changes, then install and verify them with Ansible")
		}
		if len(current.toolRequests) != 0 {
			current.report.Actions = append(current.report.Actions, "Resolve missing target tool versions and publisher checksums before confirmation, then install with Ansible")
		}
		if view.Context.Name != "" {
			current.report.Actions = append(current.report.Actions, "Verify or establish the selected controller binding")
		}
		return current, nil
	}
	current.host, err = s.host.Identity(ctx)
	if err != nil {
		return current, err
	}
	if view.State.Host.Valid() && !view.State.Host.Equal(current.host) {
		return current, failure("controller.identity", "stored setup belongs to a different installed host", "restore the original host and state; setup cannot rebind it")
	}
	current.report.setCheck(readiness("installed-host", "verified local identity", true))
	if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
		if !current.compatibleReceipt(view.State.Receipt) || !current.matchesActions(view.State.Receipt.Actions) {
			return current, failure("controller.unknown", "another exact setup attempt remains unresolved", "restore its original context, executable and acquisition route, then repeat bastion setup")
		}
	}
	if view.OpenBundle != nil && current.toolsResolved {
		area, err := view.OpenBundle(ctx, current.definition.CatalogDigest)
		if err != nil {
			return current, err
		}
		if area != nil {
			current.bundle, err = s.bundle.Inspect(ctx, area, current.definition, true)
			if err != nil {
				return current, err
			}
			current.reusable = current.definition.Bootstrap != nil
		}
	}
	current.report.setCheck(readiness("execution-bundle", current.versions(), current.bundle.Ready))
	installTools := false
	if len(current.toolRequests) != 0 {
		current.report.setCheck(readiness("target-tools", "desired-state native clients", current.toolsResolved && current.bundle.ToolsReady))
		installTools = !current.toolsResolved || !current.bundle.ToolsReady
	}
	if !current.bundle.Ready {
		current.report.Actions = append(current.report.Actions, "Prepare and verify the pinned execution bundle")
	}
	if current.selection.ContainerRuntime() {
		current.runtime, err = s.inspectRuntime(ctx, current.definition)
		if err != nil {
			return current, err
		}
		current.report.setCheck(readiness("container-runtime", current.definition.Runtime.Version, current.runtime.Ready))
		if !current.runtime.Ready {
			current.report.Actions = append(current.report.Actions, "Run the Ansible bastion role to install and verify missing native prerequisites")
		}
		if current.definition.Native != nil && !current.runtime.Ready {
			for _, action := range current.definition.Native.Actions {
				current.report.Actions = append(current.report.Actions, describeNativeAction(action))
			}
		}
	}
	// Target clients are installed by the same Ansible run the bundle and native
	// runtime enable, so they follow both in catalog dependency order.
	if installTools {
		current.report.Actions = append(current.report.Actions, "Install and verify the desired-state native clients with Ansible")
	}
	current.bound = view.Context.Name == ""
	for _, binding := range view.State.Bindings {
		if binding.ContextID != view.Context.ID {
			continue
		}
		digest, _ := current.host.PrivateDigest()
		if binding.Machine != current.selection.MachineName() || binding.HostDigest != digest {
			return current, failure("controller.identity", "selected controller does not match its established host binding", "restore the bound controller input; setup cannot rebind it")
		}
		current.bound = true
	}
	if view.Context.Name != "" {
		current.report.setCheck(readiness("controller-binding", current.selection.MachineName(), current.bound))
		if !current.bound {
			current.report.Actions = append(current.report.Actions, "Bind the selected controller Machine to this installed host")
		}
	}
	if view.State.Receipt.ID != "" && view.State.Receipt.Incomplete() {
		current.report.Checks = append(current.report.Checks, Check{"setup-recovery", "complete", "incomplete", "not-ready"})
		current.report.Actions = append(current.report.Actions, "Resolve the exact pending setup receipt")
	} else if view.State.Receipt.ID == "" || view.State.Receipt.Status != "complete" || !current.bundle.Sealed {
		observed := view.State.Receipt.Status
		if observed == "" {
			observed = "missing"
		} else if observed == "complete" && !current.bundle.Sealed {
			observed = "selected bundle is not sealed"
		}
		current.report.Checks = append(current.report.Checks, Check{"setup-state", "complete", observed, "not-ready"})
		current.report.Actions = append(current.report.Actions, "Record verified setup completion")
	}
	return current, nil
}

func (i inspection) versions() string {
	return "Python " + i.definition.PythonVersion + ", Ansible " + i.definition.AnsibleVersion
}

func (i inspection) ready() bool {
	return i.bundle.Ready && i.bundle.Sealed && i.dependenciesReady() && i.bound && i.view.State.Receipt.ID != "" && i.view.State.Receipt.Status == "complete"
}

func (i inspection) dependenciesReady() bool {
	return (!i.selection.ContainerRuntime() || i.runtime.Ready) && i.toolsResolved && (len(i.toolRequests) == 0 || i.bundle.ToolsReady)
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

func (r *Report) setCheck(check Check) {
	for index := range r.Checks {
		if r.Checks[index].ID == check.ID {
			r.Checks[index] = check
			return
		}
	}
}

func readiness(id, required string, ready bool) Check {
	if ready {
		return Check{id, required, required, "ready"}
	}
	return Check{id, required, "missing or unverified", "not-ready"}
}

func setupCommand(name string) string {
	if name == "" {
		return "run bootwright bastion setup"
	}
	return "run bootwright bastion setup --context " + name
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
