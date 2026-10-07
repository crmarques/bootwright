package clients

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var rhelPlatform = prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"}

// installerMedia places an Anaconda installation's artifact server on the
// controller, which selects the installer-media tooling and nothing else.
func installerMedia() []api.Object {
	return []api.Object{
		api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
			WithPath(api.StringValue("lab-artifacts"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef")),
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().
			WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("rhel"), "os", "installProfileRef")),
		api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue("controller"))),
	}
}

// hostedProvider is a libvirt provider whose guests run on the controller.
func hostedProvider() api.Object {
	return api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef"))
}

// guestOnLibvirt is a Machine on a libvirt provider another host runs.
func guestOnLibvirt() []api.Object {
	return []api.Object{
		api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor-host"), "libvirt", "machineRef")),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("lab"), "substrate", "providerRef")),
	}
}

// installingInstaller is the controller automation completing its native
// transaction: the roots it was handed read as installed once it ran.
type installingInstaller struct {
	fakeInstaller
	native *fakeNative
}

func (i *installingInstaller) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	i.native.ready = true
	return i.fakeInstaller.Clients(ctx, installation)
}

func evidenceOf(t *testing.T, data json.RawMessage) Evidence {
	t.Helper()
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatalf("evidence %s: %v", data, err)
	}
	return evidence
}

func stageRequest(t *testing.T, block reconciliation.Block) Request {
	t.Helper()
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// An installer-media-only context selects no libvirt client, so its stage asks
// the solver for the installer-media tooling beside the runtime alone, retains
// that resolution, and proves its completion from it.
func TestAnInstallerMediaOnlyStageResolvesNoLibvirtClient(t *testing.T) {
	want := prerequisites.NativeRequirements{ContainerRuntime: true, InstallerMedia: true}
	tools := &fakeTools{complete: true, present: true}
	native := &fakeNative{plan: nativePlanFor(t, want)}
	capability := New(tools, native, native, &installingInstaller{native: native})
	block := planBlock(t, capability, stateOf(append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...)...), reconciliation.Apply)
	if request := stageRequest(t, block); request.LibvirtClient || !request.InstallerMedia || request.Hypervisor {
		t.Fatalf("frozen request = %+v", request)
	}
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if !slices.Equal(native.requirements, []prerequisites.NativeRequirements{want}) {
		t.Fatalf("the stage asked the solver for %+v, want only %+v", native.requirements, want)
	}
	if recorder.resolved != 1 || recorder.resolution.NativeRequirements != want {
		t.Fatalf("retained %d resolutions of %+v", recorder.resolved, recorder.resolution.NativeRequirements)
	}
	if roots := evidenceOf(t, result.Evidence).Roots; !slices.Equal(roots, []string{"installer-media"}) {
		t.Fatalf("evidence roots = %v", roots)
	}
	state := hostState(t)
	state.RetainedDefinitions = []prerequisites.Definition{*state.Receipt.Definition, recorder.resolution}
	observation, err := capability.Observe(context.Background(), newRecorder(&fakeArea{sealed: true}).execution(t, block, state))
	if err != nil || observation.Effect != reconciliation.EffectCompleted {
		t.Fatalf("observation after the stage completed = %+v (%v)", observation, err)
	}
	if roots := evidenceOf(t, observation.Evidence).Roots; !slices.Equal(roots, []string{"installer-media"}) {
		t.Fatalf("observed roots = %v", roots)
	}
}

// selections are the graphs that select a native closure, by what selects it.
func selections() map[string][]api.Object {
	return map[string][]api.Object{
		"a declared libvirt capability":    {environment(), machine("container-runtime", "libvirt")},
		"a provider hosted here":           {environment(), machine("container-runtime"), hostedProvider()},
		"a Machine on a libvirt provider":  append([]api.Object{environment(), machine("container-runtime")}, guestOnLibvirt()...),
		"an installer-media tooling alone": append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...),
	}
}

// The libvirt client the solver is asked for is exactly the one the frozen
// request names, whatever selected it, and never one another closure forces.
func TestTheFrozenRequestAndTheResolverAgreeOnTheLibvirtClient(t *testing.T) {
	for name, objects := range selections() {
		t.Run(name, func(t *testing.T) {
			native := &fakeNative{}
			capability := New(&fakeTools{complete: true, present: true}, native, native, &fakeInstaller{})
			block := planBlock(t, capability, stateOf(objects...), reconciliation.Apply)
			request := stageRequest(t, block)
			want := prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: request.LibvirtClient, Hypervisor: request.Hypervisor, InstallerMedia: request.InstallerMedia}
			native.plan = nativePlanFor(t, want)
			_, _ = capability.Apply(context.Background(), newRecorder(&fakeArea{}).execution(t, block, hostState(t)))
			if len(native.requirements) != 1 || native.requirements[0] != want {
				t.Fatalf("request %+v asked the solver for %+v", request, native.requirements)
			}
		})
	}
}

// A frozen request names the same native selection StageNativeOf reads from
// the selection it was frozen from, the libvirt intent included: a declared
// libvirt version selects no release while nothing installs libvirt.
func TestARequestAndStageNativeOfAgree(t *testing.T) {
	declared := environment(field("dependencyVersions", api.MapValue(text("libvirt", "10.0.0"))))
	cases := selections()
	cases["a declared libvirt version with only installer media"] = append([]api.Object{declared, machine("container-runtime")}, installerMedia()...)
	cases["a declared libvirt version with only target clients"] = []api.Object{declared, machine("container-runtime"), cluster()}
	cases["a declared libvirt version with a libvirt capability"] = []api.Object{declared, machine("container-runtime", "libvirt")}
	for name, objects := range cases {
		t.Run(name, func(t *testing.T) {
			catalog := stateOf(objects...).Effective()
			selection, err := controller.Select(catalog)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := controller.SelectTools(catalog)
			if err != nil {
				t.Fatal(err)
			}
			got, want := NewRequest(selection, tools).native(), prerequisites.StageNativeOf(selection)
			if got != want {
				t.Fatalf("the request reads %+v, the selection %+v", got, want)
			}
			if strings.HasPrefix(name, "a declared libvirt version") {
				libvirt := "latest"
				if want.LibvirtClient {
					libvirt = "10.0.0"
				}
				if want.Libvirt != libvirt {
					t.Fatalf("libvirt intent = %q, want %q", want.Libvirt, libvirt)
				}
			}
		})
	}
}

// A libvirt provider hosted on the controller selects the hypervisor closure
// with the client, and the stage plans, resolves, retains, installs and proves
// both, end to end.
func TestTheStageInstallsTheHostedHypervisorClosure(t *testing.T) {
	want := prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true}
	tools := &fakeTools{complete: true, present: true}
	native := &fakeNative{plan: nativePlanFor(t, want)}
	installer := &installingInstaller{native: native}
	capability := New(tools, native, native, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), hostedProvider()), reconciliation.Apply)
	for _, impact := range []string{"install-native-client libvirt latest", "install-native-closure hypervisor latest"} {
		if !slices.Contains(block.Impacts, impact) {
			t.Fatalf("impacts %v lack %q", block.Impacts, impact)
		}
	}
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if !slices.Equal(native.requirements, []prerequisites.NativeRequirements{want}) || recorder.resolved != 1 || recorder.resolution.NativeRequirements != want {
		t.Fatalf("solved %+v, retained %d resolutions of %+v", native.requirements, recorder.resolved, recorder.resolution.NativeRequirements)
	}
	if installer.publication.Definition.Native == nil || recorder.prepared != 1 || len(recorder.sealed) != 1 {
		t.Fatalf("the transaction was not published: native=%v prepared=%d sealed=%v", installer.publication.Definition.Native != nil, recorder.prepared, recorder.sealed)
	}
	evidence := evidenceOf(t, result.Evidence)
	if !evidence.Libvirt || !slices.Equal(evidence.Roots, []string{"hypervisor", "libvirt"}) {
		t.Fatalf("evidence = %+v", evidence)
	}
	state := hostState(t)
	state.RetainedDefinitions = []prerequisites.Definition{*state.Receipt.Definition, recorder.resolution}
	observation, err := capability.Observe(context.Background(), newRecorder(&fakeArea{sealed: true}).execution(t, block, state))
	if err != nil || observation.Effect != reconciliation.EffectCompleted || !slices.Equal(evidenceOf(t, observation.Evidence).Roots, []string{"hypervisor", "libvirt"}) {
		t.Fatalf("observation = %+v (%v)", observation, err)
	}
}

// A stage reads only the latest resolution of its own selection: one an
// earlier build solved with the client forced beside the installer-media
// tooling no longer serves an installer-media-only request, and the stage's
// new solve supersedes only resolutions of its own selection.
func TestAStageReadsOnlyItsOwnSelectionsLatestResolution(t *testing.T) {
	mediaOnly := prerequisites.NativeRequirements{ContainerRuntime: true, InstallerMedia: true}
	paired := resolvedDefinition(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true, InstallerMedia: true})
	earlier, later := resolvedFrom(t, mediaOnly, strings.Repeat("1", 64)), resolvedFrom(t, mediaOnly, strings.Repeat("2", 64))
	objects := append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...)
	for _, test := range []struct {
		name       string
		ready      bool
		retained   []prerequisites.Definition
		checked    string
		superseded []string
	}{
		{name: "an installed resolution of another selection", ready: true, retained: []prerequisites.Definition{paired}},
		{name: "two of its own beside another's", retained: []prerequisites.Definition{earlier, paired, later},
			checked: later.Native.Digest, superseded: []string{earlier.ResolutionDigest, later.ResolutionDigest}},
	} {
		t.Run(test.name, func(t *testing.T) {
			native := &fakeNative{plan: nativePlanFor(t, mediaOnly), ready: test.ready}
			capability := New(&fakeTools{complete: true, present: true}, native, native, &installingInstaller{native: native})
			block := planBlock(t, capability, stateOf(objects...), reconciliation.Apply)
			state := hostState(t)
			state.RetainedDefinitions = append([]prerequisites.Definition{*state.Receipt.Definition}, test.retained...)
			recorder := newRecorder(&fakeArea{})
			if _, err := capability.Apply(context.Background(), recorder.execution(t, block, state)); err != nil {
				t.Fatal(err)
			}
			if native.resolves != 1 || recorder.resolved != 1 {
				t.Fatalf("resolves=%d retained=%d: another selection's resolution served the request", native.resolves, recorder.resolved)
			}
			if slices.Contains(native.checked, paired.Native.Digest) || slices.Contains(native.checked, earlier.Native.Digest) {
				t.Fatalf("the stage read a resolution it does not own: %v", native.checked)
			}
			if test.checked != "" && (len(native.checked) == 0 || native.checked[0] != test.checked) {
				t.Fatalf("the stage read %v first, want its latest %s", native.checked, test.checked)
			}
			if superseded := slices.Sorted(slices.Values(recorder.superseded)); !slices.Equal(superseded, slices.Sorted(slices.Values(test.superseded))) {
				t.Fatalf("superseded %v, want %v", superseded, test.superseded)
			}
		})
	}
}

// preflightStorage serves one context's view of a host to preflight.
type preflightStorage struct{ view prerequisites.StorageView }

func (s preflightStorage) ReadController(_ context.Context, _ string, fn func(prerequisites.StorageView) error) error {
	return fn(s.view)
}

func (preflightStorage) MutateController(context.Context, prerequisites.SetupContext, bool, func(prerequisites.StorageTransaction) error) error {
	return diagnostics.NewFailure("controller.state", "preflight mutates nothing", "")
}

// preflightHost answers preflight as a prepared Fedora host whose execution
// bundle it does not inspect.
type preflightHost struct {
	prerequisites.BundleManager
	state *compilation.State
}

func (h preflightHost) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return h.state, nil, nil
}
func (preflightHost) Platform(context.Context) (prerequisites.Platform, error) {
	return testPlatform, nil
}
func (preflightHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
}
func (preflightHost) Admit(prerequisites.Platform) error             { return nil }
func (preflightHost) ValidateEgress(prerequisites.SetupEgress) error { return nil }

// qualifiedFoundation answers as a host holding the execution foundation the
// build pins.
type qualifiedFoundation struct{}

func (qualifiedFoundation) Inspect(context.Context, prerequisites.Platform) (prerequisites.FoundationInspection, error) {
	return prerequisites.FoundationInspection{Required: "glibc 2.42-16.fc43, libgcc 15.3.1-1.fc43"}, nil
}

// unresolvedBootstrap resolves nothing: preflight never consults a publisher.
type unresolvedBootstrap struct{}

func (unresolvedBootstrap) Resolve(context.Context, prerequisites.Platform, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.BootstrapDefinition, []diagnostics.Diagnostic, error) {
	return prerequisites.BootstrapDefinition{}, nil, diagnostics.NewFailure("controller.setup", "preflight resolved Python and Ansible", "")
}

// Preflight and the stage read one closure: over the same retained
// resolutions and the same package manager, each closure preflight reports
// present is exactly a root the stage's evidence names, and the stage
// completes exactly when preflight finds every closure present.
func TestPreflightAndTheStageReadOneClosure(t *testing.T) {
	selected := prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true}
	own := resolvedDefinition(t, selected)
	another := resolvedDefinition(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true})
	keys := map[string]string{"libvirt-client": "libvirt", "hypervisor": "hypervisor"}
	for _, test := range []struct {
		name     string
		retained []prerequisites.Definition
		absent   map[string]bool
	}{
		{name: "its own resolution installed", retained: []prerequisites.Definition{own}},
		{name: "its own resolution without the hypervisor", retained: []prerequisites.Definition{own}, absent: map[string]bool{"hypervisor": true}},
		{name: "an older installed resolution of another selection", retained: []prerequisites.Definition{another, own}, absent: map[string]bool{"hypervisor": true}},
		{name: "only another selection's resolution", retained: []prerequisites.Definition{another}},
		{name: "no resolution"},
	} {
		t.Run(test.name, func(t *testing.T) {
			native := &fakeNative{ready: true, absent: test.absent}
			objects := []api.Object{environment(), machine("container-runtime"), hostedProvider()}
			state := hostState(t)
			state.RetainedDefinitions = append([]prerequisites.Definition{*state.Receipt.Definition}, test.retained...)
			host := preflightHost{state: stateOf(objects...)}
			view := prerequisites.StorageView{Exists: true, Initialized: true, Context: prerequisites.SetupContext{Name: "lab"}, State: state}
			report, _ := prerequisites.New(preflightStorage{view}, host, host, host, host, nil, prerequisites.Options{Bootstrap: unresolvedBootstrap{}, Native: native, NativeInspector: native, Foundation: qualifiedFoundation{}}).Check(context.Background(), prerequisites.CheckRequest{ContextName: "lab"})
			if report == nil {
				t.Fatal("preflight reported nothing")
			}
			var present []string
			allReady, reported := true, 0
			for _, check := range report.Checks {
				key, closure := keys[check.ID]
				if !closure {
					continue
				}
				reported++
				if check.Status == "ready" {
					present = append(present, key)
				} else {
					allReady = false
				}
			}
			if reported != len(keys) {
				t.Fatalf("preflight reported %d of the %d selected closures: %+v", reported, len(keys), report.Checks)
			}
			capability := New(&fakeTools{complete: true, present: true}, native, native, &fakeInstaller{})
			block := planBlock(t, capability, stateOf(objects...), reconciliation.Apply)
			observation, err := capability.Observe(context.Background(), newRecorder(&fakeArea{sealed: true}).execution(t, block, state))
			if err != nil {
				t.Fatal(err)
			}
			if roots := evidenceOf(t, observation.Evidence).Roots; !slices.Equal(roots, slices.Sorted(slices.Values(present))) {
				t.Fatalf("the stage proved roots %v, preflight found %v present", roots, present)
			}
			if (observation.Effect == reconciliation.EffectCompleted) != allReady {
				t.Fatalf("the stage observed %q while preflight found every closure ready=%t", observation.Effect, allReady)
			}
		})
	}
}

// rhelHostState is a RHEL host setup completed.
func rhelHostState(t *testing.T) prerequisites.HostState {
	t.Helper()
	setup := resolvedOn(t, rhelPlatform, prerequisites.NativeRequirements{ContainerRuntime: true}, strings.Repeat("e", 64))
	return prerequisites.HostState{Receipt: prerequisites.SetupReceipt{Status: "complete", Definition: &setup}}
}

func signedTooling() prerequisites.OperatorPresence {
	return prerequisites.OperatorPresence{Ready: true, Installed: []prerequisites.NativeRootPresence{
		{Key: "installer-media", Package: prerequisites.NativeIdentity{Name: "lorax", Version: "34.9.26", Release: "1.el9", Architecture: "x86_64"}},
		{Key: "installer-media", Package: prerequisites.NativeIdentity{Name: "xorriso", Version: "1.5.4", Release: "5.el9", Architecture: "x86_64"}},
	}, Missing: []string{}, Foreign: []string{}}
}

// On RHEL the operator installs lorax and xorriso from the host's own
// repositories (D106). Proved present under the vendor key, they need no
// native transaction: the stage resolves nothing native, and an
// installer-media-only context with no target client runs nothing at all.
func TestARHELInstallerMediaStageAcceptsSignedOperatorRoots(t *testing.T) {
	for _, test := range []struct {
		name    string
		objects []api.Object
		outcome reconciliation.Outcome
	}{
		{name: "with target clients", objects: []api.Object{cluster()}, outcome: reconciliation.OutcomeChanged},
		{name: "installer media alone", outcome: reconciliation.OutcomeUnchanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
			tools := &fakeTools{resolved: resolved, present: true, complete: len(test.objects) == 0}
			native := &fakeNative{operator: signedTooling()}
			installer := &fakeInstaller{}
			capability := New(tools, native, native, installer)
			objects := append(append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...), test.objects...)
			block := planBlock(t, capability, stateOf(objects...), reconciliation.Apply)
			recorder := newRecorder(&fakeArea{})
			execution := recorder.execution(t, block, rhelHostState(t))
			result, err := capability.Apply(context.Background(), execution)
			if err != nil || result.Outcome != test.outcome {
				t.Fatalf("apply = %+v (%v)", result, err)
			}
			if native.resolves != 0 || native.checks != 0 || recorder.resolved != 0 || native.operators == 0 {
				t.Fatalf("resolves=%d checks=%d retained=%d operator reads=%d", native.resolves, native.checks, recorder.resolved, native.operators)
			}
			if installer.installs != 0 && installer.publication.Definition.Native != nil {
				t.Fatal("the automation received a native transaction for the operator's tooling")
			}
			if wantResolves := len(test.objects); tools.resolves != wantResolves {
				t.Fatalf("tool resolutions = %d, want %d", tools.resolves, wantResolves)
			}
			if roots := evidenceOf(t, result.Evidence).Roots; !slices.Equal(roots, []string{"installer-media"}) {
				t.Fatalf("evidence roots = %v", roots)
			}
			tools.selected, tools.complete = resolved, true
			if len(test.objects) == 0 {
				tools.selected = nil
			}
			observation, err := capability.Observe(context.Background(), execution)
			if err != nil || observation.Effect != reconciliation.EffectCompleted {
				t.Fatalf("observation = %+v (%v)", observation, err)
			}
			native.operator = prerequisites.OperatorPresence{Installed: []prerequisites.NativeRootPresence{}, Missing: []string{"xorriso"}, Foreign: []string{}}
			observation, err = capability.Observe(context.Background(), execution)
			if err != nil || observation.Effect != reconciliation.EffectNoEffect {
				t.Fatalf("observation without xorriso = %+v (%v)", observation, err)
			}
		})
	}
}

// Missing or foreign operator tooling refuses before any publisher is
// contacted: no native or target-tool resolution, no retained intent and no
// automation run, naming the package and the operator's step. A package
// another key signed is already installed, so installing its name again would
// change nothing: its step removes it and installs it from the host's own
// repositories.
func TestARHELInstallerMediaStageRefusesBeforeAcquisition(t *testing.T) {
	for _, test := range []struct {
		name     string
		operator prerequisites.OperatorPresence
		names    string
		step     string
	}{
		{name: "lorax missing", operator: prerequisites.OperatorPresence{Installed: signedTooling().Installed[1:], Missing: []string{"lorax"}, Foreign: []string{}}, names: "lorax is not installed",
			step: "Install lorax and xorriso from this host's enabled Red Hat repositories (dnf install lorax xorriso)"},
		{name: "xorriso foreign", operator: prerequisites.OperatorPresence{Installed: signedTooling().Installed[:1], Missing: []string{}, Foreign: []string{"xorriso"}}, names: "xorriso is not signed by the Red Hat release key this executable qualifies",
			step: "Reinstall xorriso from this host's enabled Red Hat repositories (dnf remove xorriso && dnf install lorax xorriso)"},
		{name: "lorax missing and xorriso foreign", operator: prerequisites.OperatorPresence{Installed: []prerequisites.NativeRootPresence{}, Missing: []string{"lorax"}, Foreign: []string{"xorriso"}},
			names: "lorax is not installed; xorriso is not signed by the Red Hat release key this executable qualifies",
			step:  "Reinstall xorriso from this host's enabled Red Hat repositories (dnf remove xorriso && dnf install lorax xorriso)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tools := &fakeTools{resolved: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}, present: true}
			native := &fakeNative{operator: test.operator}
			installer := &fakeInstaller{}
			capability := New(tools, native, native, installer)
			block := planBlock(t, capability, stateOf(append([]api.Object{environment(), machine("container-runtime"), cluster()}, installerMedia()...)...), reconciliation.Apply)
			recorder := newRecorder(&fakeArea{})
			execution := recorder.execution(t, block, rhelHostState(t))
			execution.Stage.Setup.Context = prerequisites.SetupContext{Name: "lab"}
			result, err := capability.Apply(context.Background(), execution)
			reported := diagnostics.Of(err)
			want := test.step + ", then run bootwright apply --stage controller --context lab."
			if result.Outcome != reconciliation.OutcomeFailed || len(reported) != 1 || reported[0].Code != "controller.unsupported" ||
				!strings.HasSuffix(reported[0].Message, "; "+test.names) || reported[0].Remediation != want {
				t.Fatalf("refusal = %+v %+v", result, reported)
			}
			if native.resolves+tools.resolves+installer.installs+len(recorder.retained)+len(recorder.areas) != 0 {
				t.Fatalf("the refusal came after acquisition began: native=%d tools=%d installs=%d retained=%d areas=%v",
					native.resolves, tools.resolves, installer.installs, len(recorder.retained), recorder.areas)
			}
		})
	}
}

// No approved source carries the libvirt client or the hypervisor closure for
// RHEL, so the stage refuses such a selection, applying or observing, before
// it reads a retained identity or the host's packages.
func TestARHELLibvirtSelectionRefusesBeforeAnyRead(t *testing.T) {
	for name, objects := range map[string][]api.Object{
		"a libvirt capability":   {environment(), machine("container-runtime", "libvirt"), cluster()},
		"a provider hosted here": {environment(), machine("container-runtime"), hostedProvider(), cluster()},
	} {
		t.Run(name, func(t *testing.T) {
			tools := &fakeTools{complete: true, present: true}
			native := &fakeNative{ready: true, operator: signedTooling()}
			capability := New(tools, native, native, &fakeInstaller{})
			block := planBlock(t, capability, stateOf(objects...), reconciliation.Apply)
			execution := newRecorder(&fakeArea{}).execution(t, block, rhelHostState(t))
			_, applied := capability.Apply(context.Background(), execution)
			observation, observed := capability.Observe(context.Background(), execution)
			for _, err := range []error{applied, observed} {
				if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "controller.unsupported" || !strings.HasPrefix(reported[0].Remediation, "Use a Fedora controller") {
					t.Fatalf("refusal = %+v", reported)
				}
			}
			if observation.Effect != reconciliation.EffectUnknown || tools.selects+native.checks+native.resolves+native.operators != 0 {
				t.Fatalf("effect=%q selects=%d checks=%d resolves=%d operator reads=%d", observation.Effect, tools.selects, native.checks, native.resolves, native.operators)
			}
		})
	}
}

var _ lifecycle.Capability = Capability{}
