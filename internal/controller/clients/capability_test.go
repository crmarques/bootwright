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
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func machine(capabilities ...string) api.Object {
	items := make([]api.Value, 0, len(capabilities))
	for _, capability := range capabilities {
		items = append(items, api.StringValue(capability))
	}
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("capabilities", api.ListValue(items...)),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
}

func environment(extra ...api.FieldValue) api.Object {
	fields := append([]api.FieldValue{field("controller", api.MapValue(text("machineRef", "controller")))}, extra...)
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(fields...))
}

func cluster() api.Object {
	return api.NewObject(api.ContainerCluster, "hub", api.Value{}, api.MapValue(
		field("distribution", api.MapValue(
			text("type", "openshift"),
			field("release", api.MapValue(text("version", "4.19.3"))),
		)),
	))
}

func stateOf(objects ...api.Object) *compilation.State {
	catalog := api.NewCatalog(objects)
	return compilation.NewState(catalog, catalog, nil)
}

func planInput(state *compilation.State, verb reconciliation.Verb) lifecycle.PlanInput {
	return lifecycle.PlanInput{Verb: verb, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "lab"}}
}

func firstCode(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return ""
	}
	return reported[0].Code
}

// fakes ------------------------------------------------------------------

type fakeArea struct {
	entries []prerequisites.BundleEntry
	sealed  bool
}

func (a *fakeArea) Read(context.Context, string, int) ([]byte, error) { return nil, nil }
func (a *fakeArea) Write(context.Context, string, []byte, bool) error { return nil }
func (a *fakeArea) EnsureDirectory(context.Context, string) error     { return nil }
func (a *fakeArea) Entries(context.Context) ([]prerequisites.BundleEntry, error) {
	return slices.Clone(a.entries), nil
}
func (a *fakeArea) Verify(context.Context) error { return nil }
func (a *fakeArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/area", Device: 1, Inode: 2, Writable: !a.sealed, Sealed: a.sealed}, nil
}

type fakeTools struct {
	selected   []prerequisites.ToolDefinition
	complete   bool
	resolved   []prerequisites.ToolDefinition
	present    bool
	resolves   int
	selects    int
	presents   int
	resolveErr error
}

func (f *fakeTools) Select([]controller.ToolRequest, []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error) {
	f.selects++
	return slices.Clone(f.selected), f.complete, nil
}

func (f *fakeTools) Resolve(context.Context, []controller.ToolRequest, prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error) {
	f.resolves++
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return slices.Clone(f.resolved), nil
}

func (f *fakeTools) Present(context.Context, prerequisites.BundleArea, []prerequisites.ToolDefinition) (bool, error) {
	f.presents++
	return f.present, nil
}

type fakeNative struct {
	plan     prerequisites.NativeResolvedPlan
	ready    bool
	resolves int
	checks   int
}

func (f *fakeNative) Resolve(context.Context, prerequisites.Platform, prerequisites.NativeRequirements, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	f.resolves++
	return f.plan, nil
}

func (f *fakeNative) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	f.checks++
	if !f.ready {
		return prerequisites.NativePresence{}, nil
	}
	return prerequisites.NativePresence{Ready: true, Installed: []prerequisites.NativeRootPresence{{Key: "libvirt"}}}, nil
}

type fakeInstaller struct {
	installs    int
	outcome     string
	err         error
	publication prerequisites.ClientInstallation
}

func (f *fakeInstaller) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	f.installs++
	f.publication = installation
	if installation.Publish != nil {
		_ = installation.Publish(ctx, prerequisites.NativePreparation{InventorySHA256: strings.Repeat("1", 64), AddedSources: []string{}})
	}
	if installation.Release != nil {
		_ = installation.Release()
	}
	if f.err != nil {
		return prerequisites.ActionResult{Outcome: f.outcome}, f.err
	}
	outcome := f.outcome
	if outcome == "" {
		outcome = "changed"
	}
	return prerequisites.ActionResult{Outcome: outcome}, nil
}

// executions --------------------------------------------------------------

func newRecorder(area *fakeArea) *recorder {
	return &recorder{area: area, published: map[string]bool{}}
}

type recorder struct {
	areas     []string
	sealed    []string
	retained  []prerequisites.DependencySource
	resolved  int
	prepared  int
	released  int
	groups    []string
	area      *fakeArea
	openErr   error
	sealErr   error
	published map[string]bool
}

func toolDefinition(kind, version string) prerequisites.ToolDefinition {
	return prerequisites.ToolDefinition{
		Kind: kind, Version: version, Archive: "tar.gz",
		Source: prerequisites.DependencySource{
			ID: "tool-" + kind + "-" + version, URL: "https://mirror.example.test/" + kind + ".tar.gz",
			SHA256: strings.Repeat("b", 64), Bytes: 1024,
		},
		Files: []prerequisites.ToolFile{{Member: kind, Path: "tools/" + kind + "/" + version + "/" + kind}},
	}
}

var testPlatform = prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}

func source(id, url string) prerequisites.DependencySource {
	return prerequisites.DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
}

// setupDefinition is the host resolution `bootwright setup` froze: the exact
// shape this stage extends, so the test proves the real composition rather
// than a stand-in.
func testBootstrap(t *testing.T) prerequisites.BootstrapDefinition {
	t.Helper()
	bootstrap, err := prerequisites.CanonicalBootstrap(prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: testPlatform,
		PythonIntent: "latest", AnsibleIntent: "latest", PythonVersion: "3.14.7", AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources: []prerequisites.DependencySource{
			source("python-3.14.7", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"),
			source("ansible-2.21.4", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"),
			source("urllib3-2.7.0", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl"),
		},
		Wheels: []prerequisites.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible-2.21.4"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3-2.7.0"}},
		Metadata: []prerequisites.DependencySource{
			source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"),
			source("ansible-metadata", "https://pypi.org/pypi/ansible-core/json"),
		},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100,
		Execution: prerequisites.ExecutionRequirement{PythonExecutable: "python/bin/python3.14",
			Files: []prerequisites.InstalledFile{}, Links: []prerequisites.InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
		AutomationDigest:  strings.Repeat("c", 64),
	})
	if err != nil {
		t.Fatalf("bootstrap fixture: %v", err)
	}
	return bootstrap
}

func resolvedDefinition(t *testing.T, requirements prerequisites.NativeRequirements) prerequisites.Definition {
	t.Helper()
	definition, err := prerequisites.NewResolvedDefinition(testBootstrap(t), nativePlanFor(t, requirements))
	if err != nil {
		t.Fatalf("resolution fixture: %v", err)
	}
	return definition
}

func setupDefinition(t *testing.T) prerequisites.Definition {
	t.Helper()
	return resolvedDefinition(t, prerequisites.NativeRequirements{ContainerRuntime: true})
}

// nativePlanFor is one solved native transaction with nothing to change, which
// is what a host that already carries its roots resolves to.
func nativePlanFor(t *testing.T, requirements prerequisites.NativeRequirements) prerequisites.NativeResolvedPlan {
	t.Helper()
	plan := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: testPlatform, Solver: "dnf5", SolverVersion: "5.2.0",
		Requests: controller.DefaultDependencyVersions(), Requirements: requirements,
		Roots: []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{},
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}},
		Actions:      []prerequisites.NativeAction{},
		BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	roots := []struct{ key, name string }{{"nmstate", "nmstate"}, {"openssh", "openssh-clients"}, {"podman", "podman"}}
	if requirements.LibvirtClient {
		roots = append(roots, struct{ key, name string }{"libvirt", "libvirt-client"})
	}
	for _, root := range roots {
		identity := prerequisites.NativeIdentity{Name: root.name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
		plan.Roots = append(plan.Roots, prerequisites.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		plan.Packages = append(plan.Packages, prerequisites.NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("f", 40), Source: source(root.name, "https://packages.example.test/fedora/"+root.name+".rpm")})
	}
	canonical, err := prerequisites.CanonicalNativePlan(plan)
	if err != nil {
		t.Fatalf("native plan fixture: %v", err)
	}
	return canonical
}

func (r *recorder) execution(t *testing.T, block reconciliation.Block, state prerequisites.HostState) lifecycle.Execution {
	t.Helper()
	if r.published == nil {
		r.published = map[string]bool{}
	}
	return lifecycle.Execution{
		Operation: "op-0000000000000000000000000000", Attempt: 1, Block: block, Area: &fakeArea{sealed: true},
		Setup: prerequisites.StorageView{
			Exists: true, Initialized: true, State: state,
			OpenBundle: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
				if !r.published[id] {
					return nil, nil
				}
				return r.area, nil
			},
		},
		ClientArea: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
			r.areas = append(r.areas, id)
			if r.openErr != nil {
				return nil, r.openErr
			}
			r.published[id] = true
			return r.area, nil
		},
		SealClientArea: func(_ context.Context, id string) error {
			if r.sealErr != nil {
				return r.sealErr
			}
			r.sealed = append(r.sealed, id)
			return nil
		},
		RetainDependencies: func(_ context.Context, definition *prerequisites.Definition, sources []prerequisites.DependencySource) error {
			r.retained = append(r.retained, sources...)
			if definition != nil {
				r.resolved++
			}
			return nil
		},
		Prepare: func(context.Context, prerequisites.NativePreparation) error {
			r.prepared++
			return nil
		},
		ReleaseFoundation: func() error { r.released++; return nil },
		Progress: func(_ context.Context, group, status string) {
			r.groups = append(r.groups, group+"="+status)
		},
	}
}

func planBlock(t *testing.T, capability Capability, state *compilation.State, verb reconciliation.Verb) reconciliation.Block {
	t.Helper()
	contribution, err := capability.Plan(context.Background(), planInput(state, verb))
	if err != nil || len(contribution.Definitions) != 1 {
		t.Fatalf("plan contributed %d blocks (%v)", len(contribution.Definitions), err)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, contribution.Definitions)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan.Blocks[0]
}

func hostState(t *testing.T, sources ...prerequisites.DependencySource) prerequisites.HostState {
	t.Helper()
	return prerequisites.HostState{
		Receipt:         prerequisites.SetupReceipt{Status: "complete", Definition: ptr(setupDefinition(t))},
		RetainedSources: sources,
	}
}

// libvirtHostState is a host whose controller stage already froze a libvirt
// client resolution, which is what later presence checks recover it from.
func libvirtHostState(t *testing.T) prerequisites.HostState {
	t.Helper()
	state := hostState(t)
	resolution := resolvedDefinition(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true})
	state.RetainedDefinitions = []prerequisites.Definition{resolution}
	state.RetainedSources = slices.Clone(resolution.Sources)
	return state
}

func ptr[T any](value T) *T { return &value }

// tests -------------------------------------------------------------------

// A context that adds nothing to the host gets no block at all, so a plan
// without controller prerequisites gains no dependency on one.
func TestPlanContributesNoBlockWithoutSelectedClients(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	contribution, err := capability.Plan(context.Background(), planInput(stateOf(environment(), machine("container-runtime")), reconciliation.Apply))
	if err != nil || len(contribution.Definitions) != 0 {
		t.Fatalf("contribution = %+v (%v)", contribution, err)
	}
}

func TestPlanFreezesTheSelectedClientClosure(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime", "libvirt"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	if block.ID != BlockID || block.Stage != reconciliation.StageController || block.Kind != Kind || block.Object != "lab" {
		t.Fatalf("block identity = %+v", block.BlockDefinition)
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !request.LibvirtClient || request.Libvirt != "latest" || request.Machine != "controller" {
		t.Fatalf("request = %+v", request)
	}
	kinds := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		kinds = append(kinds, tool.Kind)
	}
	if !slices.Equal(kinds, []string{"helm", "openshift-clients", "openshift-install"}) {
		t.Fatalf("tool kinds = %v", kinds)
	}
	if !slices.Contains(block.Impacts, "install-native-client libvirt latest") {
		t.Fatalf("impacts = %v", block.Impacts)
	}
}

// The plan is pure: the same state must freeze the identical request, because
// the digest is what a continuation re-proves.
func TestPlanIsDeterministic(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime", "libvirt"), cluster())
	first := planBlock(t, capability, state, reconciliation.Apply)
	second := planBlock(t, capability, state, reconciliation.Apply)
	if first.RequestDigest != second.RequestDigest {
		t.Fatalf("digests differ: %s %s", first.RequestDigest, second.RequestDigest)
	}
}

// A prepared host proves its clients from retained identities alone: no
// publisher metadata, no acquisition and no publication.
func TestApplyProvesAPreparedHostWithoutPublisherAccess(t *testing.T) {
	tools := &fakeTools{selected: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}, complete: true, present: true}
	native := &fakeNative{ready: true}
	installer := &fakeInstaller{}
	capability := New(tools, native, native, installer)
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[prerequisites.ToolsDigest(tools.selected)] = true
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if tools.resolves != 0 || installer.installs != 0 || len(recorder.areas) != 0 || len(recorder.retained) != 0 {
		t.Fatalf("a ready host did work: resolves=%d installs=%d areas=%v retained=%d", tools.resolves, installer.installs, recorder.areas, len(recorder.retained))
	}
}

// Installation records the identities it will acquire before it acquires them,
// publishes into the shared area rather than the sealed setup bundle, and
// seals only after the closure is proved present.
func TestApplyRetainsIntentBeforeItPublishes(t *testing.T) {
	resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	tools := &fakeTools{resolved: resolved}
	native := &fakeNative{}
	installer := &fakeInstaller{}
	capability := New(tools, native, native, installer)
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	area := &fakeArea{}
	recorder := newRecorder(area)
	tools.present = true
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	digest := prerequisites.ToolsDigest(resolved)
	if !slices.Equal(recorder.areas, []string{digest}) || !slices.Equal(recorder.sealed, []string{digest}) {
		t.Fatalf("areas=%v sealed=%v", recorder.areas, recorder.sealed)
	}
	if !slices.ContainsFunc(recorder.retained, func(item prerequisites.DependencySource) bool { return item == resolved[0].Source }) {
		t.Fatalf("the acquisition identity was not retained before publication: %+v", recorder.retained)
	}
	if recorder.resolved != 0 {
		t.Fatal("a context without libvirt retained a native resolution")
	}
	if installer.publication.Target != area || installer.publication.Execution == installer.publication.Target {
		t.Fatal("publication did not target the shared client area")
	}
	if recorder.released != 1 {
		t.Fatalf("execution foundation released %d times", recorder.released)
	}
	if installer.publication.Definition.Native != nil {
		t.Fatal("a context without libvirt carried a native transaction")
	}
	var evidence Evidence
	if json.Unmarshal(result.Evidence, &evidence) != nil || evidence.Area != digest || len(evidence.Tools) != 1 {
		t.Fatalf("evidence = %s", result.Evidence)
	}
}

// A selected libvirt client is solved against the host's current inventory and
// its resolution is retained, so presence-only readiness can prove those roots
// afterwards without resolving again.
func TestApplyResolvesAndRetainsTheLibvirtClosure(t *testing.T) {
	tools := &fakeTools{complete: true}
	native := &fakeNative{plan: nativePlanFor(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true})}
	installer := &fakeInstaller{}
	capability := New(tools, native, native, installer)
	state := stateOf(environment(), machine("container-runtime", "libvirt"))
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	tools.present = true
	native.ready = false
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState(t)))
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("an unproved native postcondition must stay unknown: %+v (%v)", result, err)
	}
	if native.resolves != 1 || recorder.resolved != 1 {
		t.Fatalf("resolves=%d retained resolutions=%d", native.resolves, recorder.resolved)
	}
	if recorder.prepared != 1 {
		t.Fatalf("the native before-state was published %d times", recorder.prepared)
	}
	if len(recorder.sealed) != 0 {
		t.Fatal("an unproved closure was sealed")
	}
}

// The complete closure is completion; anything less is a positive absence
// whose retry is the same idempotent publication.
func TestObserveResolvesFromPresenceAlone(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	tools := &fakeTools{selected: selected, complete: true}
	native := &fakeNative{}
	capability := New(tools, native, native, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	for _, expected := range []struct {
		present bool
		effect  reconciliation.EffectState
	}{{true, reconciliation.EffectCompleted}, {false, reconciliation.EffectNoEffect}} {
		tools.present = expected.present
		recorder := newRecorder(&fakeArea{sealed: true})
		recorder.published[prerequisites.ToolsDigest(selected)] = true
		observation, err := capability.Observe(context.Background(), recorder.execution(t, block, hostState(t)))
		if err != nil || observation.Effect != expected.effect {
			t.Fatalf("present=%v effect=%q (%v)", expected.present, observation.Effect, err)
		}
	}
}

// Observation never publishes: it opens the area read-only through the setup
// view, so it can neither reserve nor create one.
func TestObservePublishesNothing(t *testing.T) {
	tools := &fakeTools{complete: true, selected: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}}
	native := &fakeNative{}
	capability := New(tools, native, native, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	if _, err := capability.Observe(context.Background(), recorder.execution(t, block, hostState(t))); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(recorder.areas) != 0 || len(recorder.sealed) != 0 || len(recorder.retained) != 0 || tools.resolves != 0 {
		t.Fatalf("observation published: areas=%v sealed=%v retained=%d resolves=%d", recorder.areas, recorder.sealed, len(recorder.retained), tools.resolves)
	}
}

// Clients outlive the context that selected them, so removal retains them.
func TestDestroyRetainsSharedClients(t *testing.T) {
	installer := &fakeInstaller{}
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, installer)
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Destroy)
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Destroy(context.Background(), recorder.execution(t, block, hostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if installer.installs != 0 || len(recorder.areas) != 0 {
		t.Fatal("removal touched the shared host closure")
	}
	var evidence Evidence
	if json.Unmarshal(result.Evidence, &evidence) != nil || !evidence.Retained {
		t.Fatalf("evidence = %s", result.Evidence)
	}
}

// A host with no completed setup has no foundation to extend.
func TestApplyRefusesAnUnpreparedHost(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	execution := recorder.execution(t, block, prerequisites.HostState{})
	execution.Setup.Initialized = false
	_, err := capability.Apply(context.Background(), execution)
	if firstCode(err) != "controller.identity" {
		t.Fatalf("code = %q (%v)", firstCode(err), err)
	}
}

// A block frozen by another build must not be reinterpreted by this one.
func TestDecodeRefusesAnotherRequestVersion(t *testing.T) {
	_, err := DecodeRequest([]byte(`{"egress":{"httpProxy":"","httpsProxy":"","noProxy":[]},"libvirt":"","libvirtClient":false,"machine":"controller","tools":[],"version":"controller-clients-v0"}`))
	if firstCode(err) != "lifecycle.state" {
		t.Fatalf("code = %q (%v)", firstCode(err), err)
	}
}

// A frozen native transaction binds the exact before-inventory it was solved
// from, so a retained resolution whose roots are gone is solved again rather
// than replayed against an inventory that has moved.
func TestApplySolvesAgainWhenANativeRootIsMissing(t *testing.T) {
	tools := &fakeTools{complete: true, present: true}
	native := &fakeNative{plan: nativePlanFor(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true})}
	installer := &fakeInstaller{}
	capability := New(tools, native, native, installer)
	state := stateOf(environment(), machine("container-runtime", "libvirt"))
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	if _, err := capability.Apply(context.Background(), recorder.execution(t, block, libvirtHostState(t))); err == nil {
		t.Fatal("an unproved native postcondition reported success")
	}
	if native.resolves != 1 {
		t.Fatalf("a missing root was replayed rather than solved again: resolves=%d", native.resolves)
	}
}

// Roots that are already installed need no transaction at all, so the request
// the automation receives carries none and only the clients are published.
func TestApplyInstallsNoNativeTransactionWhenTheRootsArePresent(t *testing.T) {
	resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	tools := &fakeTools{resolved: resolved, present: true}
	native := &fakeNative{ready: true}
	installer := &fakeInstaller{}
	capability := New(tools, native, native, installer)
	state := stateOf(environment(), machine("container-runtime", "libvirt"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, libvirtHostState(t)))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if native.resolves != 0 || recorder.resolved != 0 {
		t.Fatalf("installed roots were solved again: resolves=%d retained=%d", native.resolves, recorder.resolved)
	}
	if installer.publication.Definition.Native != nil {
		t.Fatal("the automation received a native transaction with nothing to change")
	}
	var evidence Evidence
	if json.Unmarshal(result.Evidence, &evidence) != nil || !evidence.Libvirt || !slices.Contains(evidence.Roots, "libvirt") {
		t.Fatalf("evidence = %s", result.Evidence)
	}
}

// A controller stage that ran under the version before this one is removable
// here: its frozen bytes read back as this shape, selecting neither closure,
// because neither existed when it was frozen and so neither was installed.
func TestAFrozenStageRequestOfThePriorVersionUpgradesIntoThisOne(t *testing.T) {
	prior := requestV1{
		Egress:        prerequisites.SetupEgress{NoProxy: []string{}},
		Libvirt:       "latest",
		LibvirtClient: true,
		Machine:       "controller",
		Tools:         []ToolRequest{},
		Version:       PriorVersion,
	}
	data, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatalf("the prior version was unreadable: %v", err)
	}
	if decoded.Version != Version {
		t.Fatalf("version = %q", decoded.Version)
	}
	if !decoded.LibvirtClient || decoded.Hypervisor || decoded.InstallerMedia {
		t.Fatalf("the upgrade invented a closure this stage never installed: %+v", decoded)
	}
	if decoded.Machine != "controller" || decoded.Libvirt != "latest" {
		t.Fatalf("the upgrade changed what the stage installed: %+v", decoded)
	}
	if _, err := decoded.Canonical(); err != nil {
		t.Fatalf("the upgraded request is not canonical: %v", err)
	}
}

// A version this build never wrote and no longer reads refuses, and says which.
func TestAFrozenStageRequestOlderThanOneVersionRefuses(t *testing.T) {
	_, err := DecodeRequest([]byte(`{"version":"controller-clients-v0"}`))
	if err == nil {
		t.Fatal("an unreadable version was accepted")
	}
	reported := diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Message, "controller-clients-v0") {
		t.Fatalf("the refusal did not name the version: %v", err)
	}
}
