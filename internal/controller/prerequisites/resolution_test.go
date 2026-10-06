package prerequisites

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type resolvingFixture struct {
	owner                                    *fixture
	bootstrap                                BootstrapDefinition
	native                                   NativeResolvedPlan
	bootstrapCalls, nativeCalls, inspections int
	bootstrapError, nativeError              error
	bootstrapWarnings                        []diagnostics.Diagnostic
	beforeResolve                            func()
	installedRelease                         string
}

type resolvingNative struct{ owner *resolvingFixture }

func (f *resolvingFixture) Resolve(_ context.Context, platform Platform, versions controller.DependencyVersions, _ SetupEgress) (BootstrapDefinition, []diagnostics.Diagnostic, error) {
	f.bootstrapCalls++
	f.owner.events = append(f.owner.events, "resolve-bootstrap")
	if f.beforeResolve != nil {
		f.beforeResolve()
	}
	if platform != f.bootstrap.Platform || versions.Python != f.bootstrap.PythonIntent || versions.Ansible != f.bootstrap.AnsibleIntent {
		return BootstrapDefinition{}, nil, errors.New("wrong bootstrap version intent")
	}
	return f.bootstrap, f.bootstrapWarnings, f.bootstrapError
}

func (n resolvingNative) Resolve(_ context.Context, platform Platform, requirements NativeRequirements, versions controller.DependencyVersions, _ SetupEgress) (NativeResolvedPlan, error) {
	f := n.owner
	f.nativeCalls++
	f.owner.events = append(f.owner.events, "resolve-native")
	if platform != f.native.Platform || versions != f.native.Requests || requirements != f.native.Requirements {
		return NativeResolvedPlan{}, errors.New("wrong native version intent")
	}
	return f.native, f.nativeError
}

func (f *resolvingFixture) Check(_ context.Context, plan NativeResolvedPlan) (NativePresence, error) {
	f.inspections++
	presence := NativePresence{Ready: f.owner.host.runtime.Ready}
	for _, root := range plan.Roots {
		if !presence.Ready {
			break
		}
		installed := root.Package
		if f.installedRelease != "" {
			installed.Release = f.installedRelease
		}
		presence.Installed = append(presence.Installed, NativeRootPresence{Key: root.Key, Package: installed})
	}
	return presence, nil
}

// OperatorRoots answers as a host whose operator installed nothing.
func (f *resolvingFixture) OperatorRoots(_ context.Context, _ Platform, names []string) (OperatorPresence, error) {
	return OperatorPresence{Installed: []NativeRootPresence{}, Missing: slices.Clone(names), Foreign: []string{}}, nil
}

func dynamicFixture(t *testing.T, platform ...Platform) (*fixture, *resolvingFixture) {
	t.Helper()
	f := newFixture(t, platform...)
	return f, f.resolution
}

// wireResolution gives a fixture the composed resolution ports, so its journeys
// exercise the same configuration the composition root builds.
func wireResolution(t *testing.T, f *fixture) (*fixture, *resolvingFixture) {
	t.Helper()
	// Each supported matrix owns its native solver; the shape validator pins
	// DNF4 to RHEL 9 and DNF5 to Fedora.
	solver, solverVersion, release, repository := "dnf4", "4.14.0", "1.el9", "https://packages.example.test/rhel"
	if f.host.platform.OS == "fedora" {
		solver, solverVersion, release, repository = "dnf5", "5.2.0", "1.fc43", "https://packages.example.test/fedora"
	}
	r := &resolvingFixture{owner: f}
	versions := controller.DefaultDependencyVersions()
	source := func(id, url string) DependencySource {
		return DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	bootstrap := BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: f.host.platform,
		PythonIntent: versions.Python, AnsibleIntent: versions.Ansible, PythonVersion: "3.14.7", AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources:          []DependencySource{source("python-3.14.7", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"), source("ansible-2.21.4", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"), source("urllib3-2.7.0", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl")},
		Wheels:           []BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible-2.21.4"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3-2.7.0"}},
		Metadata:         []DependencySource{source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"), source("ansible-metadata", "https://pypi.org/simple/ansible-core/")},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100,
		Execution:         ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Files: []InstalledFile{}, Links: []InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
		AutomationDigest:  strings.Repeat("c", 64),
	}
	var err error
	r.bootstrap, err = CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	native := NativeResolvedPlan{Format: "bootwright.native-plan-v1", Platform: f.host.platform, Solver: solver, SolverVersion: solverVersion, Requests: versions, Requirements: NativeRequirements{ContainerRuntime: true}, Roots: []NativeRoot{}, Repositories: []NativeRepository{{ID: "base", BaseURL: repository, MetadataSHA256: strings.Repeat("d", 64)}}, Packages: []NativePackage{}, Actions: []NativeAction{}, BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64)}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := NativeIdentity{Name: root.name, Version: "1.2.3", Release: release, Architecture: "x86_64"}
		native.Roots = append(native.Roots, NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40), Source: source(root.name, repository+"/"+root.name+".rpm")})
	}
	r.native, err = CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	f.service.options.Bootstrap = r
	f.service.options.Native = resolvingNative{r}
	f.service.options.NativeInspector = r
	f.resolution = r
	return f, r
}

// A warning the resolution returns reaches the result of the setup that
// resolved, whether it completes or stops after its plan, and a later setup
// that reuses the retained resolution consults no publisher and reports none.
func TestAResolutionWarningReachesTheSetupResult(t *testing.T) {
	warning := diagnostics.Diagnostic{Severity: "warning", Code: "controller.unsupported", Message: "the publisher's Index API page is version 1.5, newer than the 1.4 this build reads"}
	f, r := dynamicFixture(t)
	r.bootstrapWarnings = []diagnostics.Diagnostic{warning}
	f.confirmationError = failure("controller.setup", "setup was declined", "")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err == nil || report == nil || report.Outcome != "planned" || !reflect.DeepEqual(report.Warnings, r.bootstrapWarnings) {
		t.Fatalf("a declined setup lost its resolution warning: %#v %v", report, err)
	}
	f.confirmationError = nil
	report, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 2 || !reflect.DeepEqual(report.Warnings, r.bootstrapWarnings) {
		t.Fatalf("a completed setup lost its resolution warning: %#v %v", report, err)
	}
	report, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" || r.bootstrapCalls != 2 || len(report.Warnings) != 0 {
		t.Fatalf("a retained resolution repeated a warning it did not read: %#v %v", report, err)
	}
}

// A setup that its own dependency resolution stops, before any plan, still
// reports the warning of what it read and did not refuse.
func TestAResolutionWarningReachesTheResultOfASetupItStops(t *testing.T) {
	warning := diagnostics.Diagnostic{Severity: "warning", Code: "controller.unsupported", Message: "the publisher's Index API page is version 1.5, newer than the 1.4 this build reads"}
	for _, kind := range []string{"bootstrap", "native"} {
		f, r := dynamicFixture(t)
		r.bootstrapWarnings = []diagnostics.Diagnostic{warning}
		stopped := errors.New(kind + " resolution unavailable")
		if kind == "bootstrap" {
			r.bootstrapError = stopped
		} else {
			r.nativeError = stopped
		}
		report, err := f.service.Setup(context.Background(), SetupRequest{})
		if !errors.Is(err, stopped) || report == nil || report.Outcome != "planned" || report.PlanPresented || !reflect.DeepEqual(report.Warnings, r.bootstrapWarnings) {
			t.Fatalf("a setup its %s resolution stopped lost the resolution warning: %#v %v", kind, report, err)
		}
	}
}

func TestLatestSetupResolvesOnceAndReusesRetainedNoop(t *testing.T) {
	f, r := dynamicFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 1 || r.nativeCalls != 1 || f.store.state.Receipt.Definition == nil {
		t.Fatalf("initial resolution: %#v %v", report, err)
	}
	confirm := slices.Index(f.events, "confirm")
	if slices.Index(f.events, "resolve-bootstrap") >= confirm || slices.Index(f.events, "resolve-native") >= confirm {
		t.Fatal("resolution happened after confirmation")
	}
	writes, prepares := f.store.writes, f.bundle.prepares
	prior := CloneDefinition(*f.store.state.Receipt.Definition)
	// A ready controller never checks for newer releases.
	r.bootstrapError = errors.New("latest endpoint must not be contacted")
	r.nativeError = errors.New("repository must not be refreshed")
	report, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" || r.bootstrapCalls != 1 || r.nativeCalls != 1 || f.store.writes != writes || f.bundle.prepares != prepares || !SameDefinition(prior, *f.store.state.Receipt.Definition) {
		t.Fatalf("retained latest no-op: %#v %v bootstrap=%d native=%d", report, err, r.bootstrapCalls, r.nativeCalls)
	}
}

// unqualifiedFirstSetup completes a first setup whose latest resolution an
// earlier build could have written: a valid record at ansible-core 2.20.9,
// below the qualified minor but above the recorded floor. The resolver then
// offers the qualified release it would select today.
func unqualifiedFirstSetup(t *testing.T) (*fixture, *resolvingFixture) {
	t.Helper()
	f, r := dynamicFixture(t)
	qualified := r.bootstrap
	earlier, err := ansibleRecordedAt(qualified, "2.20.9")
	if err != nil {
		t.Fatal(err)
	}
	r.bootstrap = earlier
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if receipt := f.store.state.Receipt; receipt.Status != "complete" || receipt.Definition == nil || receipt.Definition.AnsibleVersion != "2.20.9" {
		t.Fatalf("the first setup recorded no complete 2.20.9 resolution: %#v", receipt)
	}
	r.bootstrap = qualified
	return f, r
}

func TestRetainedLatestOutsideTheQualifiedSetResolvesFresh(t *testing.T) {
	f, r := unqualifiedFirstSetup(t)
	// The fake bundle is not keyed by resolution; a fresh resolution's bundle
	// does not exist until setup publishes it.
	f.bundle.ready, f.bundle.sealed = false, false
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 2 || f.store.state.Receipt.Definition == nil || f.store.state.Receipt.Definition.AnsibleVersion != "2.21.4" {
		t.Fatalf("an unqualified latest resolution was reused: %#v %v bootstrap=%d", report, err, r.bootstrapCalls)
	}
	for _, value := range []struct {
		ansibleIntent, ansible, python string
		superseded                     bool
	}{
		{"latest", "2.19.0", "3.14.7", true},
		{"latest", "2.20.9", "3.14.7", true},
		{"latest", "2.22.0", "3.14.7", true},
		{"latest", "2.21.4", "3.11.9", true},
		{"latest", "2.21.4", "3.15.0", true},
		{"latest", "2.21.0", "3.14.7", false},
		{"latest", "2.21.4", "3.14.7", false},
		{"2.18.0", "2.18.0", "3.14.7", false},
		{"latest", "2.21.4", "3.12.0", false},
	} {
		definition := Definition{Versions: controller.DependencyVersions{Python: "latest", Ansible: value.ansibleIntent}, PythonVersion: value.python, AnsibleVersion: value.ansible}
		if supersededLatest(definition) != value.superseded {
			t.Fatalf("Ansible %s intent at %s on Python %s: superseded != %v", value.ansibleIntent, value.ansible, value.python, value.superseded)
		}
	}
}

// A record outside the qualified set whose automation also moved is resolved
// fresh: carrying it forward would keep a release this build does not select.
func TestSupersededAutomationNeverCarriesAnUnqualifiedRelease(t *testing.T) {
	f, r := unqualifiedFirstSetup(t)
	previous := f.store.state.Receipt.CatalogDigest
	superseded := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
	f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: previous, err: superseded}
	f.bundle.ready, f.bundle.sealed = false, false
	f.bundle.automation = strings.Repeat("9", 64)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 2 || f.bundle.rebases != 0 || f.store.state.Receipt.Definition == nil || f.store.state.Receipt.Definition.AnsibleVersion != "2.21.4" {
		t.Fatalf("an unqualified release was carried forward: %#v %v bootstrap=%d rebases=%d", report, err, r.bootstrapCalls, f.bundle.rebases)
	}
}

// A first setup on a clean host has resolved nothing, so no identity names a
// retained bundle. The durable store admits only a resolved identity, so asking
// it for one before resolving refuses the whole inspection.
func TestUnresolvedSetupRequestsNoBundleIdentity(t *testing.T) {
	f, r := dynamicFixture(t)
	report, err := f.service.Check(context.Background(), CheckRequest{})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || r.bootstrapCalls != 0 {
		t.Fatalf("unresolved inspection: %#v %v", report, err)
	}
	if len(f.store.bundleRequests) != 0 {
		t.Fatalf("unresolved inspection asked for a bundle: %q", f.store.bundleRequests)
	}
	index := slices.IndexFunc(report.Checks, func(check Check) bool { return check.ID == "execution-bundle" })
	if index == -1 || report.Checks[index].Status != "not-ready" {
		t.Fatalf("execution bundle check: %#v", report.Checks)
	}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.store.bundleRequests) == 0 {
		t.Fatal("resolved setup verified no bundle")
	}
	for _, id := range f.store.bundleRequests {
		if decoded, decodeErr := hex.DecodeString(id); decodeErr != nil || len(decoded) != 32 {
			t.Fatalf("resolved setup asked for %q", id)
		}
	}
}

func TestDependencyResolutionDryRunAndPreflightDoNotResolve(t *testing.T) {
	f, r := dynamicFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
	if err != nil || report.Outcome != "planned" || !slices.Contains(report.Dependencies, "python=latest") {
		t.Fatalf("dry-run: %#v %v", report, err)
	}
	report, err = f.service.Check(context.Background(), CheckRequest{})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || r.bootstrapCalls != 0 || r.nativeCalls != 0 || f.store.writes != 0 {
		t.Fatalf("inspection effects: %#v %v", report, err)
	}
}

func TestPendingDependencyResolutionNeverRefreshes(t *testing.T) {
	f, r := dynamicFixture(t)
	f.bundle.err = errors.New("interrupted publication")
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err == nil || f.store.state.Receipt.Definition == nil || !f.store.state.Receipt.Incomplete() {
		t.Fatal("missing frozen pending resolution")
	}
	r.bootstrapError = errors.New("latest endpoint must not be contacted")
	r.nativeError = errors.New("repository must not be refreshed")
	f.bundle.err = nil
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 1 || r.nativeCalls != 1 {
		t.Fatalf("retry changed resolution: %#v %v", report, err)
	}
}

type obsoleteBundle struct {
	BundleManager
	digest string
	err    error
	// onRefusal runs as the incompatibility is reported, so a test can stage
	// what the host looks like once setup acts on it.
	onRefusal func()
}

func (b obsoleteBundle) Inspect(ctx context.Context, area BundleArea, definition Definition, probe bool) (BundleInspection, error) {
	if definition.CatalogDigest == b.digest {
		if b.onRefusal != nil {
			b.onRefusal()
		}
		return BundleInspection{}, b.err
	}
	return b.BundleManager.Inspect(ctx, area, definition, probe)
}

// Validate and Prepare refuse that definition as Inspect does, because the
// executable judges it before it reads any area.
func (b obsoleteBundle) Validate(definition Definition) error {
	if definition.CatalogDigest == b.digest {
		return b.err
	}
	return b.BundleManager.Validate(definition)
}

func (b obsoleteBundle) Prepare(ctx context.Context, area, retained BundleArea, definition Definition, egress SetupEgress, progress func(ProgressEvent)) (BundleInspection, error) {
	if definition.CatalogDigest == b.digest {
		return BundleInspection{}, b.err
	}
	return b.BundleManager.Prepare(ctx, area, retained, definition, egress, progress)
}

// An executable whose embedded automation moved needs a new bundle, not new
// dependencies. Setup reprojects the closure the host already holds, so no
// publisher or repository is consulted, every release it froze survives, and
// preparation is offered the retained bundle to read those bytes from.
func TestSupersededAutomationCarriesTheRetainedResolutionForward(t *testing.T) {
	f, r := dynamicFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	previous := f.store.state.Receipt.CatalogDigest
	retained := CloneDefinition(*f.store.state.Receipt.Definition)
	superseded := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
	f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: previous, err: superseded}
	f.bundle.ready, f.bundle.sealed = false, false
	f.bundle.automation = strings.Repeat("9", 64)
	r.bootstrapError = errors.New("no publisher may be contacted for a carried resolution")
	r.nativeError = errors.New("no repository may be refreshed for a carried resolution")
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("carrying the resolution forward: %#v %v", report, err)
	}
	if r.bootstrapCalls != 1 || r.nativeCalls != 1 || f.bundle.rebases != 1 {
		t.Fatalf("resolution was repeated: bootstrap=%d native=%d rebases=%d", r.bootstrapCalls, r.nativeCalls, f.bundle.rebases)
	}
	carried := f.store.state.Receipt.Definition
	if carried == nil || carried.CatalogDigest == previous || carried.Bootstrap.AutomationDigest != f.bundle.automation {
		t.Fatalf("the carried resolution kept its superseded identity: %#v", carried)
	}
	if !slices.Equal(carried.Sources, retained.Sources) || carried.PythonVersion != retained.PythonVersion ||
		carried.AnsibleVersion != retained.AnsibleVersion || !reflect.DeepEqual(carried.Native, retained.Native) {
		t.Fatalf("carrying the resolution forward moved more than its automation: %#v", carried)
	}
	if !slices.Contains(f.bundle.retainedSeeds, true) {
		t.Fatal("preparation was not offered the retained bundle to read")
	}
}

// A setup that failed or was canceled is over: the next one publishes a new
// receipt rather than resuming it. An executable whose embedded automation
// moved carries that receipt's resolution forward exactly as it would a
// completed one's, so the host is never left refusing every setup.
func TestSupersededAutomationCarriesASettledReceiptForward(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			f, r := dynamicFixture(t)
			f.host.runtime = RuntimeInspection{}
			installer := &testRuntimeInstaller{owner: f, err: failure("controller.unsupported", "native transaction refused before effects", "prepare the required foundation"), result: ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}}
			f.service.runtime = installer
			if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); code(err) != "controller.unsupported" || f.store.state.Receipt.Status != "failed" {
				t.Fatalf("the first setup did not fail terminally: %v %#v", err, f.store.state.Receipt)
			}
			if status == "canceled" {
				f.store.state.Receipt.Status = "canceled"
				f.store.state.Receipt.Actions[1].Outcome = "canceled"
			}
			settled := f.store.state.Receipt
			retained := CloneDefinition(*settled.Definition)
			superseded := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
			f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: settled.CatalogDigest, err: superseded}
			f.bundle.ready, f.bundle.sealed = false, false
			f.bundle.automation = strings.Repeat("9", 64)
			r.bootstrapError = errors.New("no publisher may be contacted for a carried resolution")
			installer.err, installer.result = nil, ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("a %s setup refused the moved automation: %#v %v", status, report, err)
			}
			next := f.store.state.Receipt
			if next.ID == settled.ID || next.Status != "complete" || next.CatalogDigest == settled.CatalogDigest || next.Definition == nil || next.Definition.Bootstrap.AutomationDigest != f.bundle.automation {
				t.Fatalf("the %s receipt was not replaced by the carried resolution: %#v", status, next)
			}
			if r.bootstrapCalls != 1 || f.bundle.rebases != 1 || !slices.Contains(f.bundle.retainedSeeds, true) {
				t.Fatalf("the resolution was not carried from the retained bundle: bootstrap=%d rebases=%d seeds=%v", r.bootstrapCalls, f.bundle.rebases, f.bundle.retainedSeeds)
			}
			if !slices.Equal(next.Definition.Sources, retained.Sources) || next.Definition.PythonVersion != retained.PythonVersion ||
				next.Definition.AnsibleVersion != retained.AnsibleVersion || !reflect.DeepEqual(next.Definition.Native, retained.Native) {
				t.Fatalf("carrying the %s resolution forward moved more than its automation: %#v", status, next.Definition)
			}
		})
	}
}

// supersededRetainedBundle completes a first setup and then moves the embedded
// automation, so the next setup carries that resolution forward from the
// bundle it sealed. A fresh resolution names the moved automation, as a
// publisher resolution under the running executable does.
func supersededRetainedBundle(t *testing.T) (*fixture, *resolvingFixture, obsoleteBundle) {
	t.Helper()
	f, r := dynamicFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	superseded := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
	refused := obsoleteBundle{BundleManager: &f.bundle, digest: f.store.state.Receipt.CatalogDigest, err: superseded}
	f.service.bundle = refused
	f.bundle.ready, f.bundle.sealed = false, false
	f.bundle.automation = strings.Repeat("9", 64)
	r.bootstrap.AutomationDigest = f.bundle.automation
	var err error
	if r.bootstrap, err = CanonicalBootstrap(r.bootstrap); err != nil {
		t.Fatal(err)
	}
	return f, r, refused
}

// loseRetainedSource makes the retained bundle unable to serve what the
// carry-forward reads: one of its sources, or the whole bundle once inspection
// has found it superseded.
func loseRetainedSource(f *fixture, refused obsoleteBundle, cause string) {
	switch cause {
	case "source":
		f.bundle.rebaseErr = errors.Join(ErrRetainedSourceUnavailable, failure("controller.state", "retained dependency source cannot be read", ""))
	case "bundle":
		refused.onRefusal = func() { f.store.hidden = refused.digest }
		f.service.bundle = refused
	}
}

// A retained bundle that lost a source has nothing to carry. Setup resolves
// afresh instead of refusing every run: its publishers are consulted again,
// the new plan is presented and confirmed, and preparation is offered no
// retained bundle, so it acquires and verifies every source from its publisher.
func TestCarryForwardResolvesAfreshFromARetainedBundleThatLostASource(t *testing.T) {
	for _, cause := range []string{"source", "bundle"} {
		t.Run(cause, func(t *testing.T) {
			f, r, refused := supersededRetainedBundle(t)
			loseRetainedSource(f, refused, cause)
			before, events := f.store.state.Receipt, len(f.events)
			report, err := f.service.Setup(context.Background(), SetupRequest{})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("a retained bundle that lost a source refused setup: %#v %v", report, err)
			}
			rebases := map[string]int{"source": 1, "bundle": 0}[cause]
			if r.bootstrapCalls != 2 || r.nativeCalls != 2 || f.bundle.rebases != rebases {
				t.Fatalf("setup did not resolve afresh: bootstrap=%d native=%d rebases=%d", r.bootstrapCalls, r.nativeCalls, f.bundle.rebases)
			}
			if seeds := f.bundle.retainedSeeds; len(seeds) != 2 || seeds[1] {
				t.Fatalf("the fresh resolution was offered the retained bundle: %v", seeds)
			}
			next := f.store.state.Receipt
			if next.ID == before.ID || next.Status != "complete" || next.CatalogDigest == before.CatalogDigest || next.Definition == nil || next.Definition.Bootstrap.AutomationDigest != f.bundle.automation {
				t.Fatalf("the fresh resolution was not recorded: %#v", next)
			}
			for _, check := range report.Checks {
				if check.Status != "ready" {
					t.Fatalf("setup completed over an unverified check: %#v", report.Checks)
				}
			}
			order := f.events[events:]
			resolve, present, confirm, prepare := slices.Index(order, "resolve-bootstrap"), slices.Index(order, "present"), slices.Index(order, "confirm"), slices.Index(order, "prepare")
			if resolve == -1 || present < resolve || confirm < present || prepare < confirm || slices.Index(order, "rebase") > resolve {
				t.Fatalf("the fresh resolution did not run before its plan and preparation: %q", order)
			}
		})
	}
}

// The fresh resolution a lost source falls back to is any fresh resolution: a
// publisher that fails, or that now serves other bytes under a retained
// source's identity, refuses exactly as it does for a host whose retained
// bundle was already gone when setup inspected it, before any plan or effect.
func TestCarryForwardFallbackRefusesAsAFreshResolutionWould(t *testing.T) {
	unavailable := failure("controller.setup", "the dependency publisher could not be reached", "")
	for _, publisher := range []string{"unavailable", "moved"} {
		t.Run(publisher, func(t *testing.T) {
			outcomes := map[string]error{}
			for _, host := range []string{"lost", "gone"} {
				f, r, refused := supersededRetainedBundle(t)
				if host == "lost" {
					loseRetainedSource(f, refused, "source")
				} else {
					f.store.hidden = refused.digest
				}
				switch publisher {
				case "unavailable":
					r.bootstrapError = unavailable
				case "moved":
					r.bootstrap.Sources[0].SHA256 = strings.Repeat("e", 64)
					var err error
					if r.bootstrap, err = CanonicalBootstrap(r.bootstrap); err != nil {
						t.Fatal(err)
					}
				}
				before, writes, prepares, events := f.store.state.Receipt, f.store.writes, f.bundle.prepares, len(f.events)
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
				if err == nil || r.bootstrapCalls != 2 || f.store.writes != writes || f.bundle.prepares != prepares || !reflect.DeepEqual(f.store.state.Receipt, before) {
					t.Fatalf("%s: a failed fresh resolution crossed an effect boundary: %v", host, err)
				}
				if slices.Contains(f.events[events:], "present") || slices.Contains(f.events[events:], "confirm") {
					t.Fatalf("%s: a failed fresh resolution presented a plan: %q", host, f.events[events:])
				}
				outcomes[host] = err
			}
			if lost, gone := diagnostics.Of(outcomes["lost"]), diagnostics.Of(outcomes["gone"]); len(lost) == 0 || !reflect.DeepEqual(lost, gone) {
				t.Fatalf("the fallback refused unlike a fresh resolution: %v, want %v", outcomes["lost"], outcomes["gone"])
			}
			if publisher == "unavailable" && !errors.Is(outcomes["lost"], unavailable) || publisher == "moved" && code(outcomes["lost"]) != "controller.identity" {
				t.Fatalf("the fallback did not report the publisher's refusal: %v", outcomes["lost"])
			}
		})
	}
}

// A reprojection that fails for any other cause than a source the bundle lost
// still refuses: a fresh resolution would not settle it.
func TestCarryForwardRefusesAReprojectionThatFailsForAnotherCause(t *testing.T) {
	f, r, _ := supersededRetainedBundle(t)
	f.bundle.rebaseErr = failure("controller.state", "retained Python projection lacks its declared executable", "")
	writes, prepares := f.store.writes, f.bundle.prepares
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if !errors.Is(err, f.bundle.rebaseErr) || r.bootstrapCalls != 1 || f.store.writes != writes || f.bundle.prepares != prepares {
		t.Fatalf("a failed reprojection was replaced by a fresh resolution: %#v %v", report, err)
	}
}

// An incompatibility a reprojection cannot settle, such as a different
// provided execution foundation, still needs a whole new resolution. Only a
// settled receipt, whose setup completed, failed or was canceled, may be
// replaced that way: an unfinished or corrupt one protects the setup it
// belongs to.
func TestIncompatibleFoundationResolvesFreshOnlyFromASettledReceipt(t *testing.T) {
	for _, state := range []string{"complete", "failed", "canceled", "pending", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			f, r := dynamicFixture(t)
			if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
				t.Fatal(err)
			}
			previous := f.store.state.Receipt.CatalogDigest
			incompatible := errors.Join(ErrBootstrapIncompatible, failure("controller.unsupported", "unqualified execution foundation", "use the original executable"))
			settled := state == "complete" || state == "failed" || state == "canceled"
			switch state {
			case "failed", "canceled":
				f.store.state.Receipt.Status = state
				f.store.state.Receipt.Actions[1].Outcome = state
			case "pending":
				f.store.state.Receipt.Status = "pending"
			case "corrupt":
				incompatible = errors.New("bundle bytes changed")
			}
			f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: previous, err: incompatible}
			f.bundle.ready, f.bundle.sealed = false, false
			r.bootstrap.AutomationDigest = strings.Repeat("d", 64)
			var err error
			r.bootstrap, err = CanonicalBootstrap(r.bootstrap)
			if err != nil {
				t.Fatal(err)
			}
			writes := f.store.writes
			if _, err := f.service.Check(context.Background(), CheckRequest{}); err == nil || r.bootstrapCalls != 1 || f.store.writes != writes {
				t.Fatal("preflight refreshed incompatible automation")
			}
			report, err := f.service.Setup(context.Background(), SetupRequest{})
			if settled {
				if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 2 || f.store.state.Receipt.CatalogDigest == previous || f.store.state.Receipt.Status != "complete" {
					t.Fatalf("fresh automation selection failed: %#v %v", report, err)
				}
			} else if err == nil || r.bootstrapCalls != 1 || f.store.writes != writes {
				t.Fatalf("pending or corrupt bundle was bypassed: %#v %v", report, err)
			}
		})
	}
}

func TestDependencyResolutionFailurePreventsConfirmationAndMutation(t *testing.T) {
	for _, kind := range []string{"bootstrap", "native", "host"} {
		t.Run(kind, func(t *testing.T) {
			f, r := dynamicFixture(t)
			switch kind {
			case "bootstrap":
				r.bootstrapError = errors.New("unavailable")
			case "native":
				r.nativeError = errors.New("unavailable")
			case "host":
				r.beforeResolve = func() {
					f.host.identity, _ = controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("2", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
				}
			}
			_, err := f.service.Setup(context.Background(), SetupRequest{})
			if err == nil || f.store.writes != 0 || f.bundle.prepares != 0 || slices.Contains(f.events, "present") || slices.Contains(f.events, "confirm") {
				t.Fatalf("resolution failure crossed effect boundary: %v", err)
			}
		})
	}
}

func TestNativeFoundationReplacementRefusesBeforeConfirmation(t *testing.T) {
	for _, name := range []string{"glibc", "libgcc"} {
		t.Run(name, func(t *testing.T) {
			f, r := dynamicFixture(t)
			before := NativeIdentity{Name: name, Version: "1.0", Release: "1.el9", Architecture: "x86_64"}
			after := before
			after.Version = "2.0"
			source := DependencySource{ID: name, URL: "https://packages.example.test/rhel/" + name + ".rpm", SHA256: strings.Repeat("a", 64), Bytes: 64}
			r.native.Packages = append(r.native.Packages, NativePackage{Name: name, Version: after.Version, Release: after.Release, Architecture: after.Architecture, Source: source, Signer: strings.Repeat("f", 40)})
			r.native.Actions = []NativeAction{{Kind: "upgrade", Before: &before, After: after, SourceID: source.ID, Reason: "dependency"}}
			r.native.AfterSHA256 = strings.Repeat("f", 64)
			var err error
			r.native, err = CanonicalNativePlan(r.native)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.service.Setup(context.Background(), SetupRequest{})
			if code(err) != "controller.unsupported" || f.store.writes != 0 || f.bundle.prepares != 0 || slices.Contains(f.events, "present") || slices.Contains(f.events, "confirm") {
				t.Fatalf("foundation replacement crossed effect boundary: %v", err)
			}
		})
	}
}

func TestResolvedDefinitionRejectsTamperingAndCopiesInputs(t *testing.T) {
	_, r := dynamicFixture(t)
	definition, err := NewResolvedDefinition(r.bootstrap, r.native)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResolvedDefinition(definition); err != nil {
		t.Fatal(err)
	}
	r.bootstrap.Sources[0].URL = "https://untrusted.example.test/payload"
	r.native.Roots[0].Requested = "other"
	if err := ValidateResolvedDefinition(definition); err != nil {
		t.Fatal("constructor retained mutable aliases", err)
	}
	for _, mutate := range []func(*Definition){func(d *Definition) { d.Sources[0].SHA256 = strings.Repeat("0", 64) }, func(d *Definition) { d.Native.BeforeSHA256 = strings.Repeat("0", 64) }, func(d *Definition) { d.Versions.Python = "3.13.15" }, func(d *Definition) { d.CatalogDigest = strings.Repeat("0", 64) }} {
		changed := CloneDefinition(definition)
		mutate(&changed)
		if ValidateResolvedDefinition(changed) == nil {
			t.Fatal("tampered frozen definition was accepted")
		}
	}
}

func TestResolvedDefinitionRejectsConflictingSourceIdentity(t *testing.T) {
	_, r := dynamicFixture(t)
	r.native.Packages[0].Source.ID = r.bootstrap.Sources[0].ID
	var err error
	r.native, err = CanonicalNativePlan(r.native)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewResolvedDefinition(r.bootstrap, r.native); err == nil {
		t.Fatal("native resolution replaced a bootstrap source identity")
	}
}

func TestNativeSolveRefusesChangedBytesForSameRetainedRelease(t *testing.T) {
	f, r := dynamicFixture(t)
	if _, err := f.service.Setup(t.Context(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	writes := f.store.writes
	// Only a missing native root solves again; the retained bootstrap is kept.
	f.host.runtime = RuntimeInspection{}
	r.bootstrapError = errors.New("bootstrap publisher must not be contacted")
	r.native.Packages[0].Source.SHA256 = strings.Repeat("0", 64)
	var err error
	r.native, err = CanonicalNativePlan(r.native)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Setup(t.Context(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.identity" || f.store.writes != writes {
		t.Fatalf("same native release changed publisher identity: %v", err)
	}
}

// A libvirt client is selected by one context's desired state, so it never
// enters the context-independent closure setup admits and prepares. Its own
// controller stage installs it, and preflight reports it as a context check.
// The host is Fedora, whose stage can realize that client.
func TestSelectedLibvirtClientIsNotASetupPrerequisite(t *testing.T) {
	f, r := dynamicFixture(t, Platform{"fedora", "43", "amd64"})
	f.compiler.extra = []api.Object{
		api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, api.MapValue().With("libvirt", api.MapValue())),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor"), "substrate", "providerRef")),
	}
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}

	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup refused a context's libvirt selection: %#v %v", report, err)
	}
	if frozen := f.store.state.Receipt.Definition.NativeRequirements; frozen != (NativeRequirements{ContainerRuntime: true}) {
		t.Fatalf("setup froze %#v; the closure is not context-independent", frozen)
	}
	if r.nativeCalls != 1 {
		t.Fatalf("setup solved the native transaction %d times", r.nativeCalls)
	}
	// Preflight reports the same selection as an unmet context prerequisite.
	check, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" {
		t.Fatalf("context preflight = %#v %v", check, err)
	}
	found := false
	for _, entry := range check.Checks {
		if entry.ID == "libvirt-client" {
			found = entry.Scope == ContextScope && entry.Status == "not-ready"
		}
	}
	if !found {
		t.Fatalf("libvirt client was not reported as an unmet context prerequisite: %#v", check.Checks)
	}
}

// The milestone claims both supported matrices are covered by the same
// host-independent journey, so the journey must actually run on both.
func TestSetupJourneyCoversBothSupportedMatrices(t *testing.T) {
	for _, platform := range []Platform{{"rhel", "9.8", "amd64"}, {"fedora", "43", "amd64"}} {
		t.Run(platform.OS+"-"+platform.Release, func(t *testing.T) {
			f, r := dynamicFixture(t, platform)
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 1 || r.nativeCalls != 1 {
				t.Fatalf("%v initial setup: %#v %v", platform, report, err)
			}
			if solver := f.store.state.Receipt.Definition.Native.Solver; platform.OS == "fedora" && solver != "dnf5" || platform.OS == "rhel" && solver != "dnf4" {
				t.Fatalf("%v froze the wrong native solver: %s", platform, solver)
			}
			// An unchanged rerun reuses the retained resolution as a no-op. The host
			// has since moved podman to a newer build: presence is by name, so the
			// runtime stays ready and the report shows the installed release.
			r.installedRelease = "9.newer"
			report, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err != nil || report.Outcome != "unchanged" || r.bootstrapCalls != 1 || r.nativeCalls != 1 {
				t.Fatalf("%v rerun: %#v %v", platform, report, err)
			}
			index := slices.IndexFunc(report.Checks, func(check Check) bool { return check.ID == "container-runtime" })
			if index == -1 || report.Checks[index].Status != "ready" || report.Checks[index].Observed != "1.2.3-9.newer" || report.Checks[index].Required == report.Checks[index].Observed {
				t.Fatalf("%v runtime check = %#v", platform, report.Checks)
			}
		})
	}
}
