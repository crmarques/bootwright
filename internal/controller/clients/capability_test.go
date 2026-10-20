package clients

import (
	"bytes"
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
	selectErr  error
	// offered is the retained sources the last Select was handed.
	offered []prerequisites.DependencySource
}

func (f *fakeTools) Select(_ []controller.ToolRequest, retained []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error) {
	f.selects++
	f.offered = slices.Clone(retained)
	if f.selectErr != nil {
		return nil, false, f.selectErr
	}
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

// fakeDnf is the host's package manager: installed is what rpm reports, and
// Install adds what it was asked for unless it fails. calls records every
// call in order, with the setup lock's state at the time.
type fakeDnf struct {
	installed  map[string]bool
	previewErr error
	installErr error
	egress     []prerequisites.SetupEgress
	calls      []string
	lock       *fakeSetup
	output     prerequisites.RunOutput
}

func newDnf(installed ...string) *fakeDnf {
	dnf := &fakeDnf{installed: map[string]bool{}}
	for _, name := range installed {
		dnf.installed[name] = true
	}
	return dnf
}

func (f *fakeDnf) open(egress prerequisites.SetupEgress) (prerequisites.HostPackages, error) {
	f.egress = append(f.egress, egress)
	return f, nil
}

func (f *fakeDnf) record(call string, names []string) {
	held := ""
	if f.lock != nil && f.lock.held {
		held = " (locked)"
	}
	f.calls = append(f.calls, call+" "+strings.Join(names, ",")+held)
}

func (f *fakeDnf) Qualify(context.Context) error { return nil }

func (f *fakeDnf) Missing(_ context.Context, requirements []string) ([]string, error) {
	f.record("missing", requirements)
	missing := []string{}
	for _, requirement := range requirements {
		if !f.installed[requirement] {
			missing = append(missing, requirement)
		}
	}
	return missing, nil
}

func (f *fakeDnf) Installed(_ context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	f.record("installed", requirements)
	return f.providers(requirements, ""), nil
}

func (f *fakeDnf) Present(_ context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	f.record("present", requirements)
	return f.providers(requirements, "updates"), nil
}

func (f *fakeDnf) providers(requirements []string, repository string) []prerequisites.HostPackageEvidence {
	evidence := []prerequisites.HostPackageEvidence{}
	for _, requirement := range requirements {
		if f.installed[requirement] {
			evidence = append(evidence, prerequisites.HostPackageEvidence{Requirement: requirement, Package: requirement + "-0:1.0-1.fc44.x86_64", Repository: repository})
		}
	}
	return evidence
}

func (f *fakeDnf) Preview(_ context.Context, missing []string) (prerequisites.HostPackagePreview, error) {
	f.record("preview", missing)
	if f.previewErr != nil {
		return prerequisites.HostPackagePreview{}, f.previewErr
	}
	return prerequisites.HostPackagePreview{Text: "Installing: " + strings.Join(missing, " ") + "\n"}, nil
}

func (f *fakeDnf) Install(_ context.Context, missing []string, output prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	f.record("install", missing)
	f.output = output
	if f.installErr != nil {
		return nil, f.installErr
	}
	for _, name := range missing {
		f.installed[name] = true
	}
	return nil, nil
}

// fakeSetup is setup/lock: held while its callback runs, over a setup record
// whose view is view.
type fakeSetup struct {
	view  prerequisites.SetupView
	held  bool
	takes int
	err   error
}

type fakeSetupTransaction struct {
	prerequisites.SetupTransaction
	view prerequisites.SetupView
}

func (t fakeSetupTransaction) View() prerequisites.SetupView { return t.view }

func (f *fakeSetup) MutateSetup(_ context.Context, callback func(prerequisites.SetupTransaction) error) error {
	f.takes++
	if f.err != nil {
		return f.err
	}
	f.held = true
	defer func() { f.held = false }()
	return callback(fakeSetupTransaction{view: f.view})
}

type fakeInstaller struct {
	installs    int
	outcome     string
	err         error
	publication prerequisites.ClientInstallation
	announced   error
}

func (f *fakeInstaller) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	f.installs++
	f.publication = installation
	if installation.Publish != nil {
		added := []string{}
		for _, tool := range installation.Tools {
			added = append(added, tool.Source.ID)
		}
		slices.Sort(added)
		if f.announced = installation.Publish(ctx, added); f.announced != nil {
			return prerequisites.ActionResult{Outcome: "failed"}, f.announced
		}
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

// stage builds a capability over the fakes, with the host package manager
// and setup lock wired to each other.
func stage(tools *fakeTools, dnf *fakeDnf, setup *fakeSetup, installer *fakeInstaller) Capability {
	dnf.lock = setup
	return New(tools, dnf.open, setup, installer)
}

// unwired is a capability that only plans or reads frozen blocks.
func unwired() Capability { return stage(&fakeTools{}, newDnf(), &fakeSetup{}, &fakeInstaller{}) }

// executions --------------------------------------------------------------

func newRecorder(area *fakeArea) *recorder {
	return &recorder{area: area, published: map[string]bool{}}
}

type recorder struct {
	areas        []string
	sealed       []string
	retained     []prerequisites.DependencySource
	preparations []prerequisites.StagePreparation
	groups       []string
	area         *fakeArea
	openErr      error
	sealErr      error
	retireErr    error
	// current is each area a successful stage named on its binding.
	current   []string
	published map[string]bool
	output    bytes.Buffer
	// order records each effect boundary the stage crossed, in order.
	order []string
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

func (r *recorder) execution(t *testing.T, block reconciliation.Block, state prerequisites.HostState) lifecycle.Execution {
	t.Helper()
	if r.published == nil {
		r.published = map[string]bool{}
	}
	return lifecycle.Execution{
		Operation: "op-0000000000000000000000000000", Attempt: 1, Block: block, Output: &r.output,
		Stage: &lifecycle.ControllerStage{
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
				r.order = append(r.order, "area")
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
			RetireToolAreas: func(_ context.Context, current string) error {
				r.current = append(r.current, current)
				r.order = append(r.order, "retire")
				return r.retireErr
			},
			RetainDependencies: func(_ context.Context, sources []prerequisites.DependencySource) error {
				r.retained = append(r.retained, sources...)
				r.order = append(r.order, "retain")
				return nil
			},
			Prepare: func(_ context.Context, preparation prerequisites.StagePreparation) error {
				r.preparations = append(r.preparations, preparation)
				r.order = append(r.order, "prepare")
				return nil
			},
		},
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

func hostState(sources ...prerequisites.DependencySource) prerequisites.HostState {
	return prerequisites.HostState{ToolSources: sources}
}

// tests -------------------------------------------------------------------

// A context that adds nothing to the host gets no block at all, so a plan
// without controller prerequisites gains no dependency on one.
func TestPlanContributesNoBlockWithoutSelectedClients(t *testing.T) {
	contribution, err := unwired().Plan(context.Background(), planInput(stateOf(environment(), machine("container-runtime")), reconciliation.Apply))
	if err != nil || len(contribution.Definitions) != 0 {
		t.Fatalf("contribution = %+v (%v)", contribution, err)
	}
}

// The plan freezes the selected closures and presents the host package names
// they require, without reading the host.
func TestPlanFreezesTheSelectedClientClosure(t *testing.T) {
	dnf := newDnf()
	capability := stage(&fakeTools{}, dnf, &fakeSetup{}, &fakeInstaller{})
	state := stateOf(environment(), machine("container-runtime", "libvirt"), cluster())
	block := planBlock(t, capability, state, reconciliation.Apply)
	if block.ID != BlockID || block.Stage != reconciliation.StageController || block.Kind != Kind || block.Object != "lab" {
		t.Fatalf("block identity = %+v", block.BlockDefinition)
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !request.LibvirtClient || request.Libvirt != "latest" || request.Machine != "controller" || !slices.Equal(request.Packages, []string{"libvirt-client"}) {
		t.Fatalf("request = %+v", request)
	}
	kinds := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		kinds = append(kinds, tool.Kind)
	}
	if !slices.Equal(kinds, []string{"helm", "openshift-clients", "openshift-install"}) {
		t.Fatalf("tool kinds = %v", kinds)
	}
	if !slices.Contains(block.Impacts, "install-host-package libvirt-client") {
		t.Fatalf("impacts = %v", block.Impacts)
	}
	if len(dnf.calls) != 0 || len(dnf.egress) != 0 {
		t.Fatalf("planning read the host: %v", dnf.calls)
	}
}

// The plan is pure: the same state must freeze the identical request, because
// the digest is what a continuation re-proves.
func TestPlanIsDeterministic(t *testing.T) {
	state := stateOf(environment(), machine("container-runtime", "libvirt"), cluster())
	first := planBlock(t, unwired(), state, reconciliation.Apply)
	second := planBlock(t, unwired(), state, reconciliation.Apply)
	if first.RequestDigest != second.RequestDigest {
		t.Fatalf("digests differ: %s %s", first.RequestDigest, second.RequestDigest)
	}
}

// The stage asks the host's own dnf only for what is missing of the selected
// closures: presence first, then, under setup/lock, presence again, the
// preview and the install, with the intent naming those packages durable
// before any of it. The installed NEVRAs and repositories are its evidence,
// and the observation that follows proves the closure complete.
func TestTheStageInstallsMissingClosuresThroughHostDnf(t *testing.T) {
	hypervisor := []string{
		"libvirt-client", "libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
		"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
	}
	for name, test := range map[string]struct {
		objects []api.Object
		present []string
		want    []string
	}{
		"a declared libvirt capability":   {objects: []api.Object{environment(), machine("container-runtime", "libvirt")}, want: []string{"libvirt-client"}},
		"a provider hosted here":          {objects: []api.Object{environment(), machine("container-runtime"), hostedProvider()}, present: []string{"qemu-img", "swtpm"}, want: hypervisor},
		"installer-media tooling alone":   {objects: append([]api.Object{environment(), machine("container-runtime")}, installerMedia()...), want: []string{"lorax", "xorriso"}},
		"a Machine on a libvirt provider": {objects: append([]api.Object{environment(), machine("container-runtime")}, guestOnLibvirt()...), want: []string{"libvirt-client"}},
	} {
		t.Run(name, func(t *testing.T) {
			dnf, setup, installer := newDnf(test.present...), &fakeSetup{}, &fakeInstaller{}
			capability := stage(&fakeTools{complete: true}, dnf, setup, installer)
			block := planBlock(t, capability, stateOf(test.objects...), reconciliation.Apply)
			requirements := stageRequest(t, block).Packages
			if !slices.Equal(requirements, test.want) {
				t.Fatalf("the plan froze %v, want %v", requirements, test.want)
			}
			missing := slices.DeleteFunc(slices.Clone(requirements), func(name string) bool { return slices.Contains(test.present, name) })
			recorder := newRecorder(&fakeArea{})
			result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState()))
			if err != nil || result.Outcome != reconciliation.OutcomeChanged {
				t.Fatalf("apply = %+v (%v)", result, err)
			}
			joined, locked := strings.Join(requirements, ","), strings.Join(missing, ",")+" (locked)"
			if want := []string{"installed " + joined, "missing " + locked, "preview " + locked, "install " + locked, "present " + joined}; !slices.Equal(dnf.calls, want) {
				t.Fatalf("dnf was asked\n%q\nwant\n%q", dnf.calls, want)
			}
			if setup.takes != 1 || setup.held {
				t.Fatalf("setup/lock taken %d times, held after: %v", setup.takes, setup.held)
			}
			if len(recorder.preparations) != 1 || !slices.Equal(recorder.preparations[0].Packages, missing) || len(recorder.preparations[0].AddedSources) != 0 {
				t.Fatalf("intent = %+v, want the missing packages %v alone", recorder.preparations, missing)
			}
			if installer.installs != 0 || len(recorder.areas) != 0 {
				t.Fatalf("a native-only stage ran the automation (%d) or opened areas %v", installer.installs, recorder.areas)
			}
			if !strings.HasPrefix(recorder.output.String(), "Installing: "+strings.Join(missing, " ")) || dnf.output != &recorder.output {
				t.Fatalf("the preview and install did not reach the attempt's output: %q", recorder.output.String())
			}
			evidence := evidenceOf(t, result.Evidence)
			if len(evidence.Packages) != len(requirements) || evidence.Packages[0].Repository != "updates" || !strings.HasSuffix(evidence.Packages[0].Package, ".x86_64") {
				t.Fatalf("evidence packages = %+v", evidence.Packages)
			}
			if libvirt := slices.Contains(requirements, "libvirt-client"); evidence.Libvirt != stageRequest(t, block).LibvirtClient || !libvirt && evidence.Libvirt {
				t.Fatalf("evidence libvirt = %v", evidence.Libvirt)
			}
			observation, err := capability.Observe(context.Background(), newRecorder(&fakeArea{sealed: true}).execution(t, block, hostState()))
			if err != nil || observation.Effect != reconciliation.EffectCompleted {
				t.Fatalf("observation after the stage = %+v (%v)", observation, err)
			}
		})
	}
}

// A host that already provides the selected closure and published clients
// proves it from presence and retained identities alone: no publisher
// metadata, no dnf call (rpm alone reads presence), no lock and no publication.
func TestApplyProvesAPreparedHostWithoutPublisherAccess(t *testing.T) {
	tools := &fakeTools{selected: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}, complete: true, present: true}
	dnf, setup, installer := newDnf("libvirt-client"), &fakeSetup{}, &fakeInstaller{}
	capability := stage(tools, dnf, setup, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[prerequisites.ToolsDigest(tools.selected)] = true
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState()))
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if tools.resolves != 0 || installer.installs != 0 || len(recorder.areas) != 0 || len(recorder.retained) != 0 || len(recorder.preparations) != 0 || setup.takes != 0 {
		t.Fatalf("a ready host did work: resolves=%d installs=%d areas=%v retained=%d prepared=%d locks=%d", tools.resolves, installer.installs, recorder.areas, len(recorder.retained), len(recorder.preparations), setup.takes)
	}
	if !slices.Equal(dnf.calls, []string{"installed libvirt-client"}) {
		t.Fatalf("dnf calls = %q", dnf.calls)
	}
	if packages := evidenceOf(t, result.Evidence).Packages; len(packages) != 1 || packages[0].Requirement != "libvirt-client" {
		t.Fatalf("evidence packages = %+v", packages)
	}
}

// Every successful stage ends by naming the area it proved on its binding,
// whether it published that area or found it already published, so the
// retirement that follows never takes an area a bound context still runs.
func TestEverySuccessfulStageNamesItsAreaAndRetiresTheRest(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	digest := prerequisites.ToolsDigest(selected)
	settled := &fakeTools{selected: selected, complete: true, present: true}
	capability := stage(settled, newDnf("libvirt-client"), &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[digest] = true
	if result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState())); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("settled result = %+v (%v)", result, err)
	}
	if !slices.Equal(recorder.current, []string{digest}) || len(recorder.sealed) != 0 {
		t.Fatalf("a settled stage named %v and sealed %v, want %s named and nothing sealed", recorder.current, recorder.sealed, digest[:8])
	}
	published := &fakeTools{resolved: selected, present: true}
	capability = stage(published, newDnf(), &fakeSetup{}, &fakeInstaller{})
	block = planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	recorder = newRecorder(&fakeArea{})
	if result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState())); err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("publishing result = %+v (%v)", result, err)
	}
	if !slices.Equal(recorder.current, []string{digest}) || !slices.Equal(recorder.order[len(recorder.order)-1:], []string{"retire"}) {
		t.Fatalf("a publishing stage named %v with boundaries %v, want the area named last", recorder.current, recorder.order)
	}
}

// Retirement is the stage's last step: one that fails after the area was
// sealed leaves the attempt unknown, and one that fails on a settled stage,
// which changed nothing on the host, leaves it failed.
func TestARetirementFailureKeepsTheStageOutcomeHonest(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	refusal := diagnostics.NewFailure("context.state", "retiring controller tool area could not be removed", "")
	settled := &fakeTools{selected: selected, complete: true, present: true}
	capability := stage(settled, newDnf(), &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[prerequisites.ToolsDigest(selected)] = true
	recorder.retireErr = refusal
	if result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState())); err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("settled result = %+v (%v), want failed", result, err)
	}
	capability = stage(&fakeTools{resolved: selected, present: true}, newDnf(), &fakeSetup{}, &fakeInstaller{})
	recorder = newRecorder(&fakeArea{})
	recorder.retireErr = refusal
	if result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState())); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("publishing result = %+v (%v), want unknown", result, err)
	}
}

// Installation records the identities it will acquire before it acquires them,
// publishes into the shared area, and seals only after the closure is proved
// present. The automation announces exactly the sources the stage recorded.
func TestApplyRetainsIntentBeforeItPublishes(t *testing.T) {
	resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	tools := &fakeTools{resolved: resolved, present: true}
	installer := &fakeInstaller{}
	capability := stage(tools, newDnf(), &fakeSetup{}, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	area := &fakeArea{}
	recorder := newRecorder(area)
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState()))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	digest := prerequisites.ToolsDigest(resolved)
	if !slices.Equal(recorder.areas, []string{digest}) || !slices.Equal(recorder.sealed, []string{digest}) {
		t.Fatalf("areas=%v sealed=%v", recorder.areas, recorder.sealed)
	}
	if !slices.Equal(recorder.order, []string{"retain", "prepare", "area", "retire"}) {
		t.Fatalf("effect boundaries = %v, want intent before the area", recorder.order)
	}
	if len(recorder.preparations) != 1 || !slices.Equal(recorder.preparations[0].AddedSources, []string{resolved[0].Source.ID}) || len(recorder.preparations[0].Packages) != 0 {
		t.Fatalf("intent = %+v", recorder.preparations)
	}
	if installer.publication.Target != area || installer.publication.Identity != block.RequestDigest || installer.announced != nil {
		t.Fatalf("publication = %+v (%v)", installer.publication, installer.announced)
	}
	var evidence Evidence
	if json.Unmarshal(result.Evidence, &evidence) != nil || evidence.Area != digest || len(evidence.Tools) != 1 || len(evidence.Packages) != 0 {
		t.Fatalf("evidence = %s", result.Evidence)
	}
}

// Host packages go first, under the one intent that also names the tools.
func TestApplyInstallsHostPackagesBeforeItPublishesTools(t *testing.T) {
	resolved := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	dnf := newDnf()
	capability := stage(&fakeTools{resolved: resolved, present: true}, dnf, &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Apply(context.Background(), recorder.execution(t, block, hostState()))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if len(recorder.preparations) != 1 || !slices.Equal(recorder.preparations[0].Packages, []string{"libvirt-client"}) || !slices.Equal(recorder.preparations[0].AddedSources, []string{resolved[0].Source.ID}) {
		t.Fatalf("intent = %+v", recorder.preparations)
	}
	if !slices.Contains(dnf.calls, "install libvirt-client (locked)") || !slices.Equal(recorder.order, []string{"retain", "prepare", "area", "retire"}) {
		t.Fatalf("dnf calls %q, boundaries %v", dnf.calls, recorder.order)
	}
}

// A preview that refuses, or a setup record a launch would refuse, installs
// nothing: the attempt fails with intent recorded, and a later attempt
// repeats it. An install that fails once dnf began may have changed the host,
// so it stays unknown and is resolved from live presence.
func TestAHostPackageFailureIsFailedBeforeTheInstallAndUnknownAfter(t *testing.T) {
	refusal := &prerequisites.ScopedFailure{Code: "controller.setup", Message: "no package in the host's repositories provides libvirt-client: No match for argument: libvirt-client", Correction: "Enable the repository that provides it in the host's dnf configuration"}
	for name, test := range map[string]struct {
		dnf     *fakeDnf
		setup   *fakeSetup
		outcome reconciliation.Outcome
		code    string
		install bool
	}{
		"a preview refusal": {dnf: &fakeDnf{installed: map[string]bool{}, previewErr: refusal}, setup: &fakeSetup{}, outcome: reconciliation.OutcomeFailed, code: "controller.setup"},
		"an interrupted setup": {dnf: newDnf(), setup: &fakeSetup{view: prerequisites.SetupView{Exists: true, State: prerequisites.SetupState{Pending: &prerequisites.PendingSetup{ID: "setup-000042"}}}},
			outcome: reconciliation.OutcomeFailed, code: "controller.state"},
		"a failed install": {dnf: &fakeDnf{installed: map[string]bool{}, installErr: refusal}, setup: &fakeSetup{}, outcome: reconciliation.OutcomeUnknown, code: "controller.setup", install: true},
	} {
		t.Run(name, func(t *testing.T) {
			capability := stage(&fakeTools{complete: true}, test.dnf, test.setup, &fakeInstaller{})
			block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt")), reconciliation.Apply)
			execution := newRecorder(&fakeArea{}).execution(t, block, hostState())
			execution.Stage.Setup.Context = prerequisites.SetupContext{Name: "lab"}
			result, err := capability.Apply(context.Background(), execution)
			if result.Outcome != test.outcome || firstCode(err) != test.code {
				t.Fatalf("apply = %q %v, want %q %s", result.Outcome, diagnostics.Of(err), test.outcome, test.code)
			}
			if installed := slices.ContainsFunc(test.dnf.calls, func(call string) bool { return strings.HasPrefix(call, "install ") }); installed != test.install {
				t.Fatalf("dnf calls = %q", test.dnf.calls)
			}
			if test.code == "controller.setup" && !strings.HasSuffix(diagnostics.Of(err)[0].Remediation, ", then run bootwright apply --stage controller --context lab.") {
				t.Fatalf("remedy = %q", diagnostics.Of(err)[0].Remediation)
			}
		})
	}
}

// A requirement another process installed between the stage's first reading
// and setup/lock is not asked of dnf again.
func TestALockedRereadSkipsWhatIsAlreadyInstalled(t *testing.T) {
	dnf := newDnf()
	capability := stage(&fakeTools{complete: true}, dnf, &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt")), reconciliation.Apply)
	execution := newRecorder(&fakeArea{}).execution(t, block, hostState())
	execution.Stage.Prepare = func(context.Context, prerequisites.StagePreparation) error {
		dnf.installed["libvirt-client"] = true
		return nil
	}
	result, err := capability.Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if slices.ContainsFunc(dnf.calls, func(call string) bool {
		return strings.HasPrefix(call, "preview ") || strings.HasPrefix(call, "install ")
	}) {
		t.Fatalf("dnf calls = %q", dnf.calls)
	}
}

// The route dnf receives is the one the controller Machine selected and the
// block froze.
func TestDnfReceivesTheFrozenRoute(t *testing.T) {
	dnf := newDnf("libvirt-client")
	capability := stage(&fakeTools{complete: true}, dnf, &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt")), reconciliation.Apply)
	if _, err := capability.Apply(context.Background(), newRecorder(&fakeArea{}).execution(t, block, hostState())); err != nil {
		t.Fatal(err)
	}
	if len(dnf.egress) != 1 || !reflectEqual(dnf.egress[0], stageRequest(t, block).Egress) {
		t.Fatalf("dnf received %+v", dnf.egress)
	}
}

func reflectEqual(x, y prerequisites.SetupEgress) bool {
	return x.HTTPProxy == y.HTTPProxy && x.HTTPSProxy == y.HTTPSProxy && slices.Equal(x.NoProxy, y.NoProxy)
}

// A declared libvirt release is an intent the host's repositories decide, so
// the stage refuses it rather than install another release silently.
func TestADeclaredLibvirtReleaseRefuses(t *testing.T) {
	declared := environment(field("dependencyVersions", api.MapValue(text("libvirt", "10.0.0"))))
	dnf := newDnf()
	capability := stage(&fakeTools{complete: true}, dnf, &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(declared, machine("container-runtime", "libvirt")), reconciliation.Apply)
	result, err := capability.Apply(context.Background(), newRecorder(&fakeArea{}).execution(t, block, hostState()))
	reported := diagnostics.Of(err)
	if result.Outcome != reconciliation.OutcomeFailed || len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Remediation != "remove spec.dependencyVersions.libvirt from lab" || len(dnf.calls) != 0 {
		t.Fatalf("apply = %q %+v after %q", result.Outcome, reported, dnf.calls)
	}
}

// The complete closure is completion; anything less is a positive absence
// whose retry is the same idempotent installation.
func TestObserveResolvesFromPresenceAlone(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	for _, expected := range []struct {
		tools, packages bool
		effect          reconciliation.EffectState
	}{
		{true, true, reconciliation.EffectCompleted},
		{false, true, reconciliation.EffectNoEffect},
		{true, false, reconciliation.EffectNoEffect},
	} {
		tools := &fakeTools{selected: selected, complete: true, present: expected.tools}
		dnf := newDnf()
		dnf.installed["libvirt-client"] = expected.packages
		capability := stage(tools, dnf, &fakeSetup{}, &fakeInstaller{})
		block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
		recorder := newRecorder(&fakeArea{sealed: true})
		recorder.published[prerequisites.ToolsDigest(selected)] = true
		observation, err := capability.Observe(context.Background(), recorder.execution(t, block, hostState()))
		if err != nil || observation.Effect != expected.effect {
			t.Fatalf("%+v: effect=%q (%v)", expected, observation.Effect, err)
		}
	}
}

// Observation never publishes and never runs dnf beyond presence: it opens
// the area read-only through the setup view, so it can neither reserve nor
// create one.
func TestObservePublishesNothing(t *testing.T) {
	tools := &fakeTools{complete: true, selected: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}}
	dnf, setup := newDnf(), &fakeSetup{}
	capability := stage(tools, dnf, setup, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	if _, err := capability.Observe(context.Background(), recorder.execution(t, block, hostState())); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(recorder.areas) != 0 || len(recorder.sealed) != 0 || len(recorder.retained) != 0 || tools.resolves != 0 || setup.takes != 0 || !slices.Equal(dnf.calls, []string{"installed libvirt-client"}) {
		t.Fatalf("observation acted: areas=%v sealed=%v retained=%d resolves=%d locks=%d dnf=%q", recorder.areas, recorder.sealed, len(recorder.retained), tools.resolves, setup.takes, dnf.calls)
	}
}

// Clients outlive the context that selected them, so removal retains them.
func TestDestroyRetainsSharedClients(t *testing.T) {
	installer := &fakeInstaller{}
	dnf := newDnf()
	capability := stage(&fakeTools{}, dnf, &fakeSetup{}, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Destroy)
	recorder := newRecorder(&fakeArea{})
	result, err := capability.Destroy(context.Background(), recorder.execution(t, block, hostState()))
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("result = %+v (%v)", result, err)
	}
	if installer.installs != 0 || len(recorder.areas) != 0 || len(dnf.calls) != 0 {
		t.Fatal("removal touched the shared host closure")
	}
	var evidence Evidence
	if json.Unmarshal(result.Evidence, &evidence) != nil || !evidence.Retained {
		t.Fatalf("evidence = %s", result.Evidence)
	}
}

// A removal retains the shared closure and takes nothing back, so its
// resolution runs nothing and reads no host state: it is the removal's
// completion, with exactly the evidence the removal publishes. Only a request
// it cannot read, or a resolution already cancelled, stays unknown.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	tools := &fakeTools{selected: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}, complete: true, present: true}
	dnf, installer := newDnf(), &fakeInstaller{}
	capability := stage(tools, dnf, &fakeSetup{}, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Destroy)
	recorder := newRecorder(&fakeArea{sealed: true})
	removed, err := capability.Destroy(context.Background(), recorder.execution(t, block, hostState()))
	if err != nil {
		t.Fatal(err)
	}
	groups := slices.Clone(recorder.groups)
	bare := lifecycle.Execution{Operation: "op-0000000000000000000000000000", Attempt: 1, Resolution: 1, Block: block}
	for name, execution := range map[string]lifecycle.Execution{
		"with the controller stage": recorder.execution(t, block, hostState()),
		"with no host state at all": bare,
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := capability.ObserveRemoval(context.Background(), execution)
			if err != nil || observation.Effect != reconciliation.EffectCompleted {
				t.Fatalf("removal observation = %+v (%v), want completed", observation, err)
			}
			if !bytes.Equal(observation.Evidence, removed.Evidence) {
				t.Fatalf("the resolution's evidence %s is not the %s the removal publishes", observation.Evidence, removed.Evidence)
			}
			if tools.selects+tools.presents+tools.resolves+installer.installs+len(dnf.calls) != 0 ||
				len(recorder.areas) != 0 || len(recorder.sealed) != 0 || len(recorder.retained) != 0 || !slices.Equal(recorder.groups, groups) {
				t.Fatalf("a removal's resolution ran or read something: tools=%+v dnf=%q installs=%d areas=%v groups=%v",
					tools, dnf.calls, installer.installs, recorder.areas, recorder.groups)
			}
		})
	}
	malformed := bare
	malformed.Block.Request = json.RawMessage(`{}`)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, refused := range map[string]struct {
		ctx       context.Context
		execution lifecycle.Execution
	}{
		"a malformed request":    {context.Background(), malformed},
		"a cancelled resolution": {cancelled, bare},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := capability.ObserveRemoval(refused.ctx, refused.execution)
			if err == nil || observation.Effect != reconciliation.EffectUnknown {
				t.Fatalf("removal observation = %+v (%v), want unknown with its reason", observation, err)
			}
		})
	}
}

// A failure the adapters shared with setup raise inside this stage names this
// stage, because setup installs nothing a context selects, and no route even
// for an unreachable publisher: the block froze its controller Machine's proxy
// choice, and the failed apply holding it continues only over its exact input.
func TestStageFailuresNameTheStageThatSettlesThem(t *testing.T) {
	message := "target tool publisher api.github.com could not be reached from this host"
	unreachable := &prerequisites.ScopedFailure{Message: message, Correction: "Restore this host's access to api.github.com", Routable: true}
	for _, test := range []struct {
		name  string
		tools *fakeTools
		call  func(Capability, lifecycle.Execution) error
	}{
		{"apply", &fakeTools{resolveErr: unreachable}, func(capability Capability, execution lifecycle.Execution) error {
			_, err := capability.Apply(context.Background(), execution)
			return err
		}},
		{"observe", &fakeTools{selectErr: unreachable}, func(capability Capability, execution lifecycle.Execution) error {
			_, err := capability.Observe(context.Background(), execution)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability := stage(test.tools, newDnf(), &fakeSetup{}, &fakeInstaller{})
			block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
			execution := newRecorder(&fakeArea{}).execution(t, block, hostState())
			execution.Stage.Setup.Context = prerequisites.SetupContext{Name: "lab", Revision: "rev-" + strings.Repeat("2", 32)}
			reported := diagnostics.Of(test.call(capability, execution))
			want := "Restore this host's access to api.github.com, then run bootwright apply --stage controller --context lab."
			if len(reported) != 1 || reported[0].Code != "controller.setup" || reported[0].Message != message || reported[0].Remediation != want {
				t.Fatalf("stage failure: %+v", reported)
			}
		})
	}
}

// The controller Ansible installer is shared with setup too. A failure it
// raises inside this stage keeps its own code and names this stage, not
// setup, as what settles it.
func TestStageFailuresKeepTheirCodesAndNameTheStage(t *testing.T) {
	installer := &fakeInstaller{outcome: "unknown", err: &prerequisites.ScopedFailure{Code: "controller.unknown", Message: "the Ansible operation has no complete result", Correction: "Preserve the controller state and restore the qualified controller execution environment"}}
	capability := stage(&fakeTools{resolved: []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}}, newDnf(), &fakeSetup{}, installer)
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	execution := newRecorder(&fakeArea{}).execution(t, block, hostState())
	execution.Stage.Setup.Context = prerequisites.SetupContext{Name: "lab", Revision: "rev-" + strings.Repeat("2", 32)}
	result, err := capability.Apply(context.Background(), execution)
	reported := diagnostics.Of(err)
	if result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Code != "controller.unknown" || reported[0].Source != nil ||
		!strings.HasSuffix(reported[0].Remediation, ", then run bootwright apply --stage controller --context lab.") {
		t.Fatalf("stage failure: %q %+v", result.Outcome, reported)
	}
}

// A block frozen by another build must not be reinterpreted by this one.
func TestDecodeRefusesAnotherRequestVersion(t *testing.T) {
	_, err := DecodeRequest([]byte(`{"egress":{"httpProxy":"","httpsProxy":"","noProxy":[]},"libvirt":"","libvirtClient":false,"machine":"controller","tools":[],"version":"controller-clients-v0"}`))
	if firstCode(err) != "lifecycle.state" {
		t.Fatalf("code = %q (%v)", firstCode(err), err)
	}
}

// A request whose host packages are not exactly what its closures select is
// refused, so a frozen block can never ask dnf for anything else.
func TestDecodeRefusesPackagesItsClosuresDoNotSelect(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(environment(), machine("container-runtime", "libvirt")), reconciliation.Apply)
	request := stageRequest(t, block)
	for name, packages := range map[string][]string{"another package": {"nmstate"}, "none": {}, "one more": {"libvirt-client", "lorax"}} {
		request.Packages = packages
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeRequest(data); firstCode(err) != "lifecycle.state" {
			t.Fatalf("%s: decode = %v", name, err)
		}
	}
}

// Every version but the one this build writes refuses, and the refusal names
// it so the remedy is the executable that registered the operation.
func TestAFrozenStageRequestOfAnyOtherVersionRefuses(t *testing.T) {
	for _, version := range []string{"controller-clients-v0", "controller-clients-v1", "controller-clients-v2", "controller-clients-v3", "controller-clients-v4", "controller-clients-v6"} {
		_, err := DecodeRequest([]byte(`{"version":"` + version + `"}`))
		if err == nil {
			t.Fatalf("version %q was accepted", version)
		}
		reported := diagnostics.Of(err)
		if len(reported) == 0 || !strings.Contains(reported[0].Message, version) {
			t.Fatalf("the refusal did not name %q: %v", version, err)
		}
	}
}

// The engine reaches the lookup only through its port, so the stage must keep
// satisfying it.
var _ lifecycle.ToolLocator = Capability{}

// Another block locates its clients in the area of the closure this stage
// proved in its apply, recovered only from the retained identities that proof
// names: a newer release under the same latest intent, retained by another
// context, is never offered. Anything short of a completed apply's proof of
// exactly this request and area, read as the one bounded shape it declares, or
// a closure only partly retained, names no area it may run from, even one that
// holds the file.
func TestLocateToolAnswersOnlyFromTheClosureTheStageProved(t *testing.T) {
	release := func(kind, version, digit string, members ...string) prerequisites.ToolDefinition {
		definition := toolDefinition(kind, version)
		definition.Compatibility, definition.Files = "openshift", nil
		definition.Source.SHA256 = strings.Repeat(digit, 64)
		for _, member := range members {
			definition.Files = append(definition.Files, prerequisites.ToolFile{Member: member, Path: "tools/" + kind + "/openshift/" + version + "/" + member})
		}
		return definition
	}
	closure := []prerequisites.ToolDefinition{release("openshift-clients", "4.19.3", "c", "oc", "kubectl"), release("openshift-install", "4.19.3", "e", "openshift-install")}
	newer := release("openshift-install", "4.22.3", "0", "openshift-install")
	installer := controller.InstalledTool{Kind: "openshift-install", Compatibility: "openshift", Version: "4.19.3", Executable: "openshift-install"}
	block := planBlock(t, unwired(), stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	proof := func(edit func(*Evidence)) lifecycle.BlockEvidence {
		evidence := newEvidence(block.RequestDigest, prerequisites.ToolsDigest(closure), false, closure, nil)
		if edit != nil {
			edit(&evidence)
		}
		encoded, err := evidence.encode()
		if err != nil {
			t.Fatal(err)
		}
		return lifecycle.BlockEvidence{Kind: Kind, Object: block.Object, Implementation: Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: encoded}
	}
	for name, tc := range map[string]struct {
		proved   lifecycle.BlockEvidence
		selected []prerequisites.ToolDefinition
		complete bool
		located  string
	}{
		"the closure the stage proved": {proved: proof(nil), selected: closure, complete: true, located: "/area/tools/openshift-install/openshift/4.19.3/openshift-install"},
		"a partly retained one":        {proved: proof(nil), selected: closure[1:], complete: false},
		"an apply that proved nothing": {proved: lifecycle.BlockEvidence{State: reconciliation.BlockPending}, selected: closure, complete: true},
		"a failed stage's proof": {proved: func() lifecycle.BlockEvidence {
			value := proof(nil)
			value.State = reconciliation.BlockFailed
			return value
		}(), selected: closure, complete: true},
		"a proof of another request": {proved: proof(func(e *Evidence) { e.Request = strings.Repeat("9", 64) }), selected: closure, complete: true},
		"a proof of another area":    {proved: proof(func(e *Evidence) { e.Area = strings.Repeat("9", 64) }), selected: closure, complete: true},
		"a removal's retained proof": {proved: proof(func(e *Evidence) { e.Retained = true }), selected: closure, complete: true},
		"a proof widened by a member it never declared": {proved: func() lifecycle.BlockEvidence {
			value := proof(nil)
			value.Evidence = append(value.Evidence[:len(value.Evidence)-1], `,"widened":true}`...)
			return value
		}(), selected: closure, complete: true},
		"a proof past the evidence bound": {proved: func() lifecycle.BlockEvidence {
			value := proof(nil)
			value.Evidence = append(value.Evidence, strings.Repeat(" ", 64<<10)...)
			return value
		}(), selected: closure, complete: true},
	} {
		t.Run(name, func(t *testing.T) {
			area := &fakeArea{sealed: true}
			for _, tool := range closure {
				for _, file := range tool.Files {
					area.entries = append(area.entries, prerequisites.BundleEntry{Path: file.Path, Executable: true})
				}
			}
			view := prerequisites.StorageView{
				OpenBundle: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
					if id != prerequisites.ToolsDigest(closure) {
						return nil, nil
					}
					return area, nil
				},
				State: prerequisites.HostState{ToolSources: []prerequisites.DependencySource{closure[0].Source, newer.Source, closure[1].Source}},
			}
			tools := &fakeTools{selected: tc.selected, complete: tc.complete}
			capability := stage(tools, newDnf(), &fakeSetup{}, &fakeInstaller{})
			located, err := capability.LocateTool(context.Background(), view, block, tc.proved, installer)
			if tc.located != "" {
				if err != nil || located != tc.located {
					t.Fatalf("located %q (%v), want %q", located, err, tc.located)
				}
				if !slices.Equal(tools.offered, []prerequisites.DependencySource{closure[0].Source, closure[1].Source}) {
					t.Fatalf("the closure was recovered from %+v, not only the sources its proof names", tools.offered)
				}
				return
			}
			if err == nil || firstCode(err) != "controller.state" {
				t.Fatalf("a lookup without the stage's whole proved closure located %q (%v)", located, err)
			}
		})
	}
}

func TestAnInterruptedPublicationNeedsAReplayToSeal(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	tools := &fakeTools{selected: selected, complete: true, present: true}
	capability := stage(tools, newDnf(), &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{})
	digest := prerequisites.ToolsDigest(selected)
	recorder.published[digest] = true
	execution := recorder.execution(t, block, hostState())
	observation, err := capability.Observe(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectNoEffect || len(recorder.sealed) != 0 {
		t.Fatalf("unsealed observation: %+v %v", observation, err)
	}
	result, err := capability.Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || !slices.Equal(recorder.sealed, []string{digest}) {
		t.Fatalf("replay did not seal: %+v %v %v", result, err, recorder.sealed)
	}
}

func TestInterruptedStageUsesItsPreparedSourcesBeforeAnyContextBinding(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v4.2.3")}
	newer := toolDefinition("helm", "v4.2.4")
	tools := &fakeTools{selected: selected, complete: true, present: true}
	capability := stage(tools, newDnf(), &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[prerequisites.ToolsDigest(selected)] = true
	execution := recorder.execution(t, block, prerequisites.HostState{ToolSources: []prerequisites.DependencySource{selected[0].Source, newer.Source}})
	execution.Stage.Setup.Context.Name = "example"
	prepared, err := json.Marshal(prerequisites.StagePreparation{Packages: []string{}, AddedSources: []string{selected[0].Source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	execution.ApplyPreparations = []json.RawMessage{prepared}
	observation, err := capability.Observe(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || !slices.Equal(tools.offered, []prerequisites.DependencySource{selected[0].Source}) {
		t.Fatalf("prepared recovery: %+v %v sources=%+v", observation, err, tools.offered)
	}
}

func TestFreshStageKeepsTheReleaseInItsContextBinding(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v4.2.3")}
	newer := toolDefinition("helm", "v4.2.4")
	tools := &fakeTools{selected: selected, complete: true, present: true}
	capability := stage(tools, newDnf(), &fakeSetup{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime"), cluster()), reconciliation.Apply)
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Tools = []ToolRequest{{Kind: "helm", Version: "latest"}}
	block.Request, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	digest := prerequisites.ToolsDigest(selected)
	recorder := newRecorder(&fakeArea{sealed: true, entries: []prerequisites.BundleEntry{{Path: "sources/" + selected[0].Source.ID}}})
	recorder.published[digest] = true
	execution := recorder.execution(t, block, prerequisites.HostState{ToolSources: []prerequisites.DependencySource{selected[0].Source, newer.Source}, Bindings: []prerequisites.ControllerBinding{{Context: "example", Tools: digest}}})
	execution.Stage.Setup.Context.Name = "example"
	result, err := capability.Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || !slices.Equal(tools.offered, []prerequisites.DependencySource{selected[0].Source}) {
		t.Fatalf("fresh stage: %+v %v sources=%+v", result, err, tools.offered)
	}
}
