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
)

type resolvingFixture struct {
	owner                                    *fixture
	bootstrap                                BootstrapDefinition
	native                                   NativeResolvedPlan
	bootstrapCalls, nativeCalls, inspections int
	bootstrapError, nativeError              error
	beforeResolve                            func()
	installedRelease                         string
}

type resolvingNative struct{ owner *resolvingFixture }

func (f *resolvingFixture) Resolve(_ context.Context, platform Platform, versions controller.DependencyVersions, _ SetupEgress) (BootstrapDefinition, error) {
	f.bootstrapCalls++
	f.owner.events = append(f.owner.events, "resolve-bootstrap")
	if f.beforeResolve != nil {
		f.beforeResolve()
	}
	if platform != f.bootstrap.Platform || versions.Python != f.bootstrap.PythonIntent || versions.Ansible != f.bootstrap.AnsibleIntent {
		return BootstrapDefinition{}, errors.New("wrong bootstrap version intent")
	}
	return f.bootstrap, f.bootstrapError
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

func dynamicFixture(t *testing.T, platform ...Platform) (*fixture, *resolvingFixture) {
	t.Helper()
	return wireResolution(t, newFixture(t, platform...))
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

// A retained resolution whose bundle cannot be read is not carried forward on
// assumption: the closure it names has to come from somewhere.
func TestCarryForwardRefusesWithoutTheRetainedBundleItReadsFrom(t *testing.T) {
	for _, cause := range []string{"missing", "refused"} {
		t.Run(cause, func(t *testing.T) {
			f, r := dynamicFixture(t)
			if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			previous := f.store.state.Receipt.CatalogDigest
			superseded := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
			refused := obsoleteBundle{BundleManager: &f.bundle, digest: previous, err: superseded}
			if cause == "missing" {
				// The area is gone by the time the resolution would be read
				// out of it, which is the one way carrying forward has nothing
				// to carry.
				refused.onRefusal = func() { f.store.hidden = previous }
			} else {
				f.bundle.rebaseErr = errors.New("retained source differs from its approved identity")
			}
			f.service.bundle = refused
			f.bundle.ready, f.bundle.sealed = false, false
			writes, prepares := f.store.writes, f.bundle.prepares
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err == nil || r.bootstrapCalls != 1 || f.store.writes != writes || f.bundle.prepares != prepares {
				t.Fatalf("an unreadable retained bundle was carried forward: %#v %v", report, err)
			}
		})
	}
}

// An incompatibility a reprojection cannot settle, such as a different
// provided execution foundation, still needs a whole new resolution. Only a
// completed receipt may be replaced that way: an unfinished or corrupt one
// protects the setup it belongs to.
func TestIncompatibleFoundationResolvesFreshOnlyFromACompletedReceipt(t *testing.T) {
	for _, state := range []string{"complete", "pending", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			f, r := dynamicFixture(t)
			if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
				t.Fatal(err)
			}
			previous := f.store.state.Receipt.CatalogDigest
			incompatible := errors.Join(ErrBootstrapIncompatible, failure("controller.unsupported", "unqualified execution foundation", "use the original executable"))
			if state == "pending" {
				f.store.state.Receipt.Status = "pending"
			} else if state == "corrupt" {
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
			if state == "complete" {
				if err != nil || report.Outcome != "changed" || r.bootstrapCalls != 2 || f.store.state.Receipt.CatalogDigest == previous {
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
func TestSelectedLibvirtClientIsNotASetupPrerequisite(t *testing.T) {
	f, r := dynamicFixture(t)
	f.compiler.extra = []api.Object{
		api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, api.MapValue().With("libvirt", api.MapValue())),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor"), "substrate", "providerRef")),
	}
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}

	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup refused a context's libvirt selection: %#v %v", report, err)
	}
	for _, admitted := range f.catalog.admitted {
		if admitted.LibvirtClient || !admitted.ContainerRuntime {
			t.Fatalf("setup admitted %#v; the closure is not context-independent", f.catalog.admitted)
		}
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
