package prerequisites

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type inspection struct {
	catalog   api.Catalog
	view      SetupView
	stored    StorageView
	selection controller.Selection
	release   HostRelease
	platform  Platform
	host      controller.InstalledHostIdentity
	dryRun    bool
	mirror    WheelMirror

	ansibleCore  string
	interpreter  PythonInterpreter
	abi          string
	candidates   []string
	python       string
	runtime      Runtime
	runtimeReady bool
	wheels       []RuntimeWheel
	missing      []string

	toolRequests []controller.ToolRequest
	tools        []ToolDefinition
	toolsPresent bool
	native       StageNative
	closures     int
	bound        bool

	report Report
}

const (
	familyRequirement = "the Fedora or RHEL family on amd64"
	setupComplete     = "complete"
)

// inspect verifies the host read-only. A non-empty phase streams the scope and
// each check as it settles; the inspection repeated under the setup lock
// passes none, so every check is shown once.
func (s Service) inspect(ctx context.Context, view SetupView, stored StorageView, phase string, dryRun bool) (inspection, error) {
	current := inspection{view: view, stored: stored, dryRun: dryRun, ansibleCore: s.ports.Lock.AnsibleCore,
		selection: controller.Baseline().WithAmbientRoute(s.options.AmbientRoute)}
	if err := s.selectContext(ctx, &current); err != nil {
		return current, err
	}
	release, err := s.host.(ReleaseInspector).Release(ctx)
	if err != nil {
		return current, err
	}
	current.release = release
	current.platform = Platform{OS: release.ID, Release: release.VersionID, Architecture: release.Architecture}
	current.report = Report{ContextName: stored.Context.Name, Machine: current.selection.MachineName(), Platform: current.platform,
		DryRun: dryRun, Outcome: "planned", Route: current.routeSummary(), Checks: []Check{}, Actions: []string{}}
	stream := phase != "" && !dryRun
	if stream && s.options.Presenter != nil {
		if err := s.options.Presenter.PresentControllerScope(ctx, phase, cloneReport(current.report)); err != nil {
			return current, err
		}
		current.report.ProgressPresented = true
	}
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
	current.declare(s.host, s.ports.Lock)
	if err := s.admit(ctx, &current, settle, start); err != nil {
		return current, err
	}
	if err := s.noteLoaderPreload(ctx, &current); err != nil {
		return current, err
	}
	if !dryRun {
		if err := s.settleHost(ctx, &current, settle, start); err != nil {
			return current, err
		}
	}
	if err := s.settleRuntime(ctx, &current, settle, start); err != nil {
		return current, err
	}
	if err := s.settlePackages(ctx, &current, settle, start); err != nil {
		return current, err
	}
	if !dryRun {
		current.settleSetupState(s.ports.Lock, settle)
		current.report.Table = current.table(s.ports.Lock)
	}
	return current, s.settleContext(ctx, &current, settle, start)
}

func (i *inspection) declare(host HostInspector, lock RuntimeLock) {
	checks := []Check{{"host", familyRequirement, "unverified", "unverified", HostScope}}
	if !i.dryRun {
		checks = append(checks, Check{"installed-host", "verified local identity", "unverified", "unverified", HostScope})
		if _, reports := host.(FIPSInspector); reports {
			checks = append(checks, Check{"fips-mode", "the host's FIPS mode", "unverified", "unverified", HostScope})
		}
	}
	baseline, _ := BaselineHostRequirements("")
	checks = append(checks,
		Check{"python-runtime", runtimeRequirement(lock), "unverified", "unverified", HostScope},
		Check{"host-packages", strings.Join(baseline, ", "), "unverified", "unverified", HostScope},
		Check{"setup-state", setupComplete, "unverified", "unverified", HostScope})
	if len(i.toolRequests) != 0 {
		checks = append(checks, Check{"target-tools", i.toolSummary(), "unverified", "unverified", ContextScope})
	}
	if i.native.LibvirtClient {
		checks = append(checks, Check{"libvirt-client", closureRequirement("libvirt-client"), "unverified", "unverified", ContextScope})
	}
	if i.native.Hypervisor {
		checks = append(checks, Check{"hypervisor", closureRequirement("hypervisor"), "unverified", "unverified", ContextScope})
	}
	if i.native.InstallerMedia {
		checks = append(checks, Check{"installer-media", closureRequirement("installer-media"), "unverified", "unverified", ContextScope})
	}
	if i.native.NMState {
		checks = append(checks, Check{"nmstate", closureRequirement("nmstate"), "unverified", "unverified", ContextScope})
	}
	if i.native.IPUtils {
		checks = append(checks, Check{"iputils", closureRequirement("iputils"), "unverified", "unverified", ContextScope})
	}
	if i.stored.Context.Name != "" {
		checks = append(checks, Check{"controller-binding", i.selection.MachineName(), "unverified", "unverified", ContextScope})
	}
	i.report.Checks = checks
}

func runtimeRequirement(lock RuntimeLock) string {
	return "Python " + strings.Join(lock.ABIs, ", ") + " with ansible-core " + lock.AnsibleCore
}

func (s Service) selectContext(ctx context.Context, current *inspection) error {
	if current.stored.Context.Name == "" {
		current.native = StageNativeOf(current.selection)
		return nil
	}
	state, _, err := s.compiler.Compile(ctx, current.stored.Sources)
	if err != nil {
		return err
	}
	if state == nil {
		return failure("context.input", "context input could not be admitted", "import Environment input with context update")
	}
	current.catalog = state.Effective()
	if current.selection, err = controller.Select(state.Effective()); err != nil {
		return err
	}
	current.native = StageNativeOf(current.selection)
	current.toolRequests, err = controller.SelectTools(state.Effective())
	if err != nil || len(current.toolRequests) == 0 {
		return err
	}
	if s.options.Tools == nil {
		return failure("controller.unsupported", "target tool selection is not configured", "use a compatible executable")
	}
	current.tools, _, err = SelectContextTools(ctx, s.options.Tools, current.stored, current.toolRequests)
	return InStage(err, current.stored.Context.Name)
}

// admit is fail-closed and runs before any effect: the Fedora or RHEL family
// by ID or ID_LIKE, amd64, and qualified rpm and dnf (D152.2).
func (s Service) admit(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	start("host", "checking the OS family and architecture")
	release := current.release
	observed := release.ID + " " + release.VersionID + "/" + release.Architecture
	if len(release.Like) != 0 {
		observed += " (like " + strings.Join(release.Like, " ") + ")"
	}
	refuse := func(err error) error {
		settle(Check{"host", familyRequirement, observed, "not-ready", HostScope})
		return err
	}
	if !admittedFamily(release) {
		return refuse(failure("controller.unsupported", "this host's OS "+release.ID+" is neither Fedora nor RHEL, and its ID_LIKE names neither",
			"run Bootwright on Fedora, RHEL or a derivative whose /etc/os-release ID_LIKE names fedora or rhel"))
	}
	if release.Architecture != "amd64" {
		return refuse(failure("controller.unsupported", "this host's architecture "+release.Architecture+" is not amd64", "run Bootwright on an amd64 host"))
	}
	if err := s.ports.Packages.Qualify(ctx); err != nil {
		return refuse(err)
	}
	settle(Check{"host", familyRequirement, observed, "ready", HostScope})
	if warning := untestedRelease(release, s.ports.Lock.Tested); warning != nil {
		current.report.Warnings = append(current.report.Warnings, *warning)
	}
	return nil
}

func (s Service) noteLoaderPreload(ctx context.Context, current *inspection) error {
	inspector, reports := s.host.(LoaderPreloadInspector)
	if !reports {
		return nil
	}
	preloaded, err := inspector.LoaderPreload(ctx)
	if err != nil || !preloaded {
		return err
	}
	current.report.Warnings = append(current.report.Warnings, diagnostics.Diagnostic{Severity: "warning", Code: "controller.loader-preload",
		Message: "/etc/ld.so.preload is not empty, so it loads libraries into every process, root dnf runs included; setup continues"})
	return nil
}

func admittedFamily(release HostRelease) bool {
	for _, id := range append([]string{release.ID}, release.Like...) {
		if id == "fedora" || id == "rhel" {
			return true
		}
	}
	return false
}

func tested(release HostRelease, releases []TestedRelease) bool {
	for _, candidate := range releases {
		if candidate.ID != release.ID {
			continue
		}
		for _, version := range candidate.Versions {
			if release.VersionID == version || strings.HasPrefix(release.VersionID, version+".") {
				return true
			}
		}
	}
	return false
}

// untestedRelease is informational and never refuses (D152.2).
func untestedRelease(release HostRelease, releases []TestedRelease) *diagnostics.Diagnostic {
	if len(releases) == 0 || tested(release, releases) {
		return nil
	}
	relation, list := "is not among", []string{}
	for _, candidate := range releases {
		if candidate.ID == release.ID {
			list = []string{strings.Join(candidate.Versions, ", ")}
			if newer(release.VersionID, candidate.Versions) {
				relation = "is newer than"
			}
			break
		}
		list = append(list, releaseName(candidate.ID)+" "+strings.Join(candidate.Versions, ", "))
	}
	message := fmt.Sprintf("%s %s %s the releases this build was tested on (%s); setup checks its floors and continues",
		releaseName(release.ID), release.VersionID, relation, strings.Join(list, "; "))
	return &diagnostics.Diagnostic{Severity: "warning", Code: "controller.untested-release", Message: message}
}

func newer(version string, versions []string) bool {
	major := func(value string) int {
		number, err := strconv.Atoi(strings.SplitN(value, ".", 2)[0])
		if err != nil {
			return -1
		}
		return number
	}
	if major(version) < 0 {
		return false
	}
	for _, candidate := range versions {
		if major(candidate) >= major(version) {
			return false
		}
	}
	return true
}

func releaseName(id string) string {
	switch id {
	case "fedora":
		return "Fedora"
	case "rhel":
		return "RHEL"
	}
	return id
}

func (s Service) settleHost(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	start("installed-host", "verifying local identity")
	var err error
	if current.host, err = s.host.Identity(ctx); err != nil {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return err
	}
	if stored := current.stored.State.Host; stored.Valid() && !stored.Equal(current.host) {
		settle(unverified("installed-host", "verified local identity", HostScope))
		return failure("controller.identity", "stored controller state belongs to a different installed host", "restore the original host and state; setup cannot rebind it")
	}
	settle(readiness("installed-host", "verified local identity", true, HostScope))
	inspector, reports := s.host.(FIPSInspector)
	if RequiresClusterFIPS(current.catalog) {
		if err := CheckClusterFIPS(ctx, inspector, current.catalog); err != nil {
			settle(unverified("fips-mode", "enabled for the selected FIPS cluster", HostScope))
			return err
		}
		settle(Check{ID: "fips-mode", Required: "enabled for the selected FIPS cluster", Observed: fipsEnabled, Status: "ready", Scope: HostScope})
		return nil
	}
	if !reports {
		return nil
	}
	enabled, err := inspector.FIPSMode(ctx)
	if err != nil {
		settle(unverified("fips-mode", "the host's FIPS mode", HostScope))
		return err
	}
	mode := "disabled"
	if enabled {
		mode = fipsEnabled
	}
	settle(Check{ID: "fips-mode", Required: mode, Observed: mode, Status: "ready", Scope: HostScope})
	return nil
}

// fipsEnabled is informational (D108 as D152.3 amends it).
const fipsEnabled = "enabled; the controller runtime's TLS follows the host's crypto policy, while Bootwright itself and cryptography's bundled OpenSSL stay outside this host's FIPS-validated modules"

func (s Service) settleRuntime(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	start("python-runtime", "selecting the Python interpreter")
	lock, required := s.ports.Lock, runtimeRequirement(s.ports.Lock)
	preferred := ""
	if receipt := current.view.State.Receipt; receipt != nil {
		preferred = receipt.Runtime.Interpreter
	}
	interpreter, err := s.ports.Python.Select(ctx, preferred)
	switch {
	case err == nil:
		current.interpreter, current.abi = interpreter, interpreter.ABI
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, ErrGlibcBelowFloor):
		settle(Check{"python-runtime", required, "glibc below " + lock.GlibcFloor, "not-ready", HostScope})
		return failure("controller.unsupported", "this host's glibc is older than "+lock.GlibcFloor+", which the locked controller wheels need",
			"run Bootwright on a host whose glibc is "+lock.GlibcFloor+" or newer")
	case errors.Is(err, ErrNoPythonInterpreter):
		requirements := make([]string, 0, len(lock.ABIs))
		for _, abi := range lock.ABIs {
			requirement, _ := BaselineHostRequirements(abi)
			requirements = append(requirements, requirement[len(requirement)-1])
		}
		missing, missingErr := s.ports.Packages.Missing(ctx, requirements)
		if missingErr != nil {
			settle(unverified("python-runtime", required, HostScope))
			return missingErr
		}
		if len(missing) == 0 {
			settle(Check{"python-runtime", required, "no installed interpreter qualifies", "not-ready", HostScope})
			return err
		}
		current.candidates, current.python = missing, missing[0]
		current.abi = abiOf(missing[0])
	default:
		settle(unverified("python-runtime", required, HostScope))
		return err
	}
	if current.interpreter.Path != "" && !current.dryRun && current.view.Foreign == "" {
		runtime, ready, err := s.ports.Python.Inspect(ctx, current.interpreter)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		current.runtime, current.runtimeReady = runtime, ready && err == nil
	}
	if !current.runtimeReady {
		if err = s.settleWheels(ctx, current); err != nil {
			settle(unverified("python-runtime", required, HostScope))
			return err
		}
	}
	settle(current.runtimeCheck(required))
	return nil
}

func (s Service) settleWheels(ctx context.Context, current *inspection) error {
	wheels, err := s.ports.Python.Wheels(ctx, current.abi)
	if err != nil {
		return err
	}
	if current.view.Foreign != "" {
		for index := range wheels {
			wheels[index].Cached = false
		}
	}
	current.wheels = wheels
	return nil
}

func (i *inspection) runtimeCheck(required string) Check {
	check := Check{"python-runtime", required, "", "not-ready", HostScope}
	interpreter := i.interpreter
	switch {
	case interpreter.Path == "":
		check.Observed = "no locked interpreter installed; setup installs " + i.python
	case i.dryRun:
		check.Observed, check.Status = "Python "+interpreter.Version+" ("+interpreter.Path+"); runtime not verified without privilege", "unverified"
	case i.runtimeReady:
		check.Observed, check.Status = "Python "+interpreter.Version+" ("+interpreter.Path+"), ansible-core "+i.runtime.AnsibleCore, "ready"
	default:
		check.Observed = "Python " + interpreter.Version + " (" + interpreter.Path + "); runtime not prepared"
	}
	return check
}

func abiOf(requirement string) string {
	abi, _ := PythonRequirementABI(requirement)
	return abi
}

func (s Service) settlePackages(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	start("host-packages", "querying the RPM database")
	baseline, _ := BaselineHostRequirements("")
	required := strings.Join(baseline, ", ")
	missing, err := s.ports.Packages.Missing(ctx, baseline)
	if err != nil {
		settle(unverified("host-packages", required, HostScope))
		return err
	}
	current.missing = missing
	check := readiness("host-packages", required, len(missing) == 0, HostScope)
	if len(missing) != 0 {
		check.Observed = "missing " + strings.Join(missing, ", ")
	}
	settle(check)
	return nil
}

func (i *inspection) settleSetupState(lock RuntimeLock, settle func(Check)) {
	view, receipt := i.view, i.view.State.Receipt
	observed := setupComplete
	switch {
	case view.Foreign != "":
		observed = "written by another Bootwright build (" + view.Foreign + ")"
	case view.State.Pending != nil:
		observed = "interrupted (" + view.State.Pending.ID + ")"
	case receipt == nil:
		observed = "missing"
	case receipt.Lock != lock.Digest || receipt.Runtime.Digest != i.runtime.Digest:
		observed = "recorded for another controller runtime"
	}
	status := "not-ready"
	if observed == setupComplete {
		status = "ready"
	}
	settle(Check{"setup-state", setupComplete, observed, status, HostScope})
}

// ready is host readiness: everything context-independent setup owns.
func (i inspection) ready() bool {
	receipt := i.view.State.Receipt
	return i.view.Foreign == "" && i.view.State.Pending == nil && receipt != nil && i.runtimeReady &&
		len(i.missing) == 0 && i.interpreter.Path != "" && receipt.Runtime.Digest == i.runtime.Digest && receipt.Lock == i.runtime.LockDigest
}

func (i inspection) contextReady() bool {
	if !i.ready() || i.stored.Context.Name == "" {
		return i.ready()
	}
	selected := 0
	for _, closure := range closureTable {
		if closure.selected(i.native) {
			selected++
		}
	}
	return i.bound && (len(i.toolRequests) == 0 || i.toolsPresent) && i.closures == selected
}

// next names the one command that settles what preflight found missing:
// setup for the host, and a context's controller stage for what it selects.
func (i inspection) next() string {
	name := i.report.ContextName
	if PendingScope(i.report) != ContextScope || name == "" {
		return setupInvocation
	}
	if controller.HasStage(i.selection, i.toolRequests) {
		return stageInvocation(name)
	}
	return "bootwright apply --context " + name
}

func (i inspection) routeSummary() string {
	route := i.selection.Route()
	if machine := i.selection.MachineName(); machine != "" && route.Direct() && route.Origin() == "" {
		return "direct (Machine " + machine + ")"
	}
	return route.Summary()
}

func (i inspection) route() SetupEgress {
	route := i.selection.Route()
	bypass := route.NoProxy()
	if bypass == nil {
		bypass = []string{}
	}
	return SetupEgress{HTTPProxy: route.HTTPProxy(), HTTPSProxy: route.HTTPSProxy(), NoProxy: bypass}
}

func (i inspection) toolSummary() string {
	versions := make([]string, 0, len(i.tools))
	for _, tool := range i.tools {
		versions = append(versions, tool.Kind+" "+tool.Version)
	}
	if len(versions) != 0 {
		return strings.Join(versions, ", ")
	}
	kinds := make([]string, 0, len(i.toolRequests))
	for _, request := range i.toolRequests {
		kinds = append(kinds, request.Kind)
	}
	return strings.Join(kinds, ", ")
}

// settleContext reports what one selected context still needs on this host,
// as read-only presence its own controller stage settles.
func (s Service) settleContext(ctx context.Context, current *inspection, settle func(Check), start func(string, string)) error {
	name := current.stored.Context.Name
	if name == "" {
		return nil
	}
	if len(current.toolRequests) != 0 {
		start("target-tools", "verifying the retained target tools")
		ready := false
		if len(current.tools) != 0 && current.stored.OpenBundle != nil {
			area, err := current.stored.OpenBundle(ctx, ToolsDigest(current.tools))
			if err == nil && area != nil {
				ready, err = s.options.Tools.Present(ctx, area, current.tools)
			}
			if err != nil {
				settle(unverified("target-tools", current.toolSummary(), ContextScope))
				return err
			}
		}
		settle(readiness("target-tools", current.toolSummary(), ready, ContextScope))
		current.toolsPresent = ready
	}
	for _, closure := range closureTable {
		if !closure.selected(current.native) {
			continue
		}
		start(closure.id, "querying the RPM database")
		requirements := StageHostRequirements(closureRequirements(closure.id))
		missing, err := s.ports.Packages.Missing(ctx, requirements)
		if err != nil {
			settle(unverified(closure.id, closureRequirement(closure.id), ContextScope))
			return InStage(err, name)
		}
		check := readiness(closure.id, closureRequirement(closure.id), len(missing) == 0, ContextScope)
		if len(missing) != 0 {
			check.Observed = "missing " + strings.Join(missing, ", ")
		} else {
			current.closures++
		}
		settle(check)
	}
	digest, _ := current.host.PrivateDigest()
	for _, binding := range current.stored.State.Bindings {
		if binding.Context != name {
			continue
		}
		if binding.Machine != current.selection.MachineName() || binding.HostDigest != digest {
			return failure("controller.identity", "selected controller does not match its established host binding", "restore the bound controller input; apply cannot rebind it")
		}
		current.bound = true
	}
	binding := readiness("controller-binding", current.selection.MachineName(), current.bound, ContextScope)
	if !current.bound {
		binding.Observed = "not yet bound; its first apply binds it"
	}
	settle(binding)
	return nil
}

func closureRequirements(id string) NativeRequirements {
	return NativeRequirements{LibvirtClient: id == "libvirt-client", Hypervisor: id == "hypervisor", InstallerMedia: id == "installer-media", NMState: id == "nmstate", IPUtils: id == "iputils"}
}

func closureRequirement(id string) string {
	switch id {
	case "hypervisor":
		return "hypervisor closure"
	case "iputils":
		return "virtual-IP duplicate-address probe (iputils)"
	case "nmstate":
		return "NMState network configuration generator"
	case "installer-media":
		return "installer-media tooling (lorax, xorriso)"
	}
	return "libvirt client"
}

// samePlan compares what a confirmed plan was built on with the inspection
// repeated under the setup lock.
func (i inspection) samePlan(other inspection) bool {
	pending := func(view SetupView) string {
		if view.State.Pending == nil {
			return ""
		}
		return view.State.Pending.ID
	}
	return i.host.Equal(other.host) && i.release.ID == other.release.ID && i.release.VersionID == other.release.VersionID &&
		i.interpreter.Path == other.interpreter.Path && i.runtime.Digest == other.runtime.Digest && i.runtimeReady == other.runtimeReady &&
		slices.Equal(i.missing, other.missing) && (len(i.candidates) == 0) == (len(other.candidates) == 0) &&
		i.view.Foreign == other.view.Foreign && pending(i.view) == pending(other.view) && (i.view.State.Receipt == nil) == (other.view.State.Receipt == nil)
}
