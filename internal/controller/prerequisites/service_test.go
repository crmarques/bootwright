package prerequisites

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type fixture struct {
	service           Service
	store             memoryStorage
	host              testHost
	bundle            testBundle
	catalog           testCatalog
	compiler          testCompiler
	events            []string
	scopes            []string
	confirmationError error
	presentationError error
	beforeMutation    func()
	resolution        *resolvingFixture
}

func newFixture(t *testing.T, platform ...Platform) *fixture {
	t.Helper()
	f := &fixture{}
	f.host.identity, _ = controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	f.host.platform = Platform{"rhel", "9.8", "amd64"}
	if len(platform) != 0 {
		f.host.platform = platform[0]
	}
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	f.compiler.runtime = true
	f.host.owner = f
	f.store.owner = f
	f.bundle.owner = f
	f.bundle.recoverable = true
	f.catalog.definition = Definition{CatalogDigest: strings.Repeat("a", 64), PythonVersion: "3.13.15", AnsibleVersion: "2.21.4", Sources: []DependencySource{{ID: "python", URL: "https://artifacts.example.test/python.tar.gz", SHA256: strings.Repeat("b", 64), Bytes: 10}}, Runtime: RuntimeRequirement{Version: "5.8.2"}}
	f.service = New(&f.store, &f.compiler, &f.host, &f.catalog, &f.bundle, nil, Options{Confirmer: f, Presenter: f})
	return f
}

func (f *fixture) Confirm(ctx context.Context, _, _ string) error {
	f.events = append(f.events, "confirm")
	return f.confirmationError
}
func (f *fixture) PresentControllerScope(_ context.Context, phase string, _ Report) error {
	f.scopes = append(f.scopes, phase)
	return nil
}

func (f *fixture) PresentControllerPlan(ctx context.Context, _ Report) error {
	f.events = append(f.events, "present")
	return f.presentationError
}

type memoryStorage struct {
	owner           *fixture
	state           HostState
	scope           SetupContext
	reads, writes   int
	exists          bool
	bundleExists    bool
	failPublication int
	mutationError   error
}

func (m *memoryStorage) view() StorageView {
	value := StorageView{Exists: m.exists, Initialized: m.state.Host.Valid(), Context: m.scope, State: copyState(m.state)}
	value.OpenBundle = func(context.Context, string) (BundleArea, error) {
		if m.bundleExists {
			return dummyArea{}, nil
		}
		return nil, nil
	}
	return value
}
func (m *memoryStorage) ReadController(ctx context.Context, name string, fn func(StorageView) error) error {
	m.reads++
	m.owner.events = append(m.owner.events, "read:"+name)
	view := m.view()
	if name == "" {
		view.Context = SetupContext{}
	} else if name != m.scope.Name {
		return errors.New("wrong explicit context")
	}
	return fn(view)
}
func (m *memoryStorage) MutateController(ctx context.Context, scope SetupContext, create bool, fn func(StorageTransaction) error) error {
	m.owner.events = append(m.owner.events, "mutate")
	if m.owner.beforeMutation != nil {
		m.owner.beforeMutation()
	}
	m.exists = true
	if m.mutationError != nil {
		return m.mutationError
	}
	return fn(m)
}
func (m *memoryStorage) Snapshot() StorageView { return m.view() }
func (m *memoryStorage) Publish(ctx context.Context, state HostState) (Publication, error) {
	m.writes++
	if m.writes == m.failPublication {
		return Unknown, errors.New("private sync error")
	}
	m.state = copyState(state)
	m.state.RetainedSources = slices.Clone(state.Receipt.Sources)
	if state.Receipt.Status == "complete" {
		m.owner.bundle.sealed = true
	}
	return Committed, nil
}
func (m *memoryStorage) Bundle(ctx context.Context, _ string) (BundleArea, error) {
	if !slices.ContainsFunc(m.state.Receipt.Actions, func(a SetupAction) bool { return a.Phase == "intent" }) {
		return nil, errors.New("missing durable intent")
	}
	m.bundleExists = true
	return dummyArea{}, nil
}
func copyState(s HostState) HostState {
	s.Bindings = slices.Clone(s.Bindings)
	s.RetainedSources = slices.Clone(s.RetainedSources)
	s.Receipt.Actions = slices.Clone(s.Receipt.Actions)
	s.Receipt.Sources = slices.Clone(s.Receipt.Sources)
	s.Receipt.Egress.NoProxy = slices.Clone(s.Receipt.Egress.NoProxy)
	return s
}

type dummyArea struct{}

func (dummyArea) Read(context.Context, string, int) ([]byte, error) { return nil, nil }
func (dummyArea) Write(context.Context, string, []byte, bool) error { return nil }
func (dummyArea) EnsureDirectory(context.Context, string) error     { return nil }
func (dummyArea) Verify(context.Context) error                      { return nil }
func (dummyArea) Location(context.Context) (BundleLocation, error)  { return BundleLocation{}, nil }
func (dummyArea) Entries(context.Context) ([]BundleEntry, error)    { return nil, nil }

type testHost struct {
	owner                *fixture
	identity             controller.InstalledHostIdentity
	platform             Platform
	runtime              RuntimeInspection
	identities, runtimes int
}

func (h *testHost) Platform(context.Context) (Platform, error) {
	h.owner.events = append(h.owner.events, "platform")
	return h.platform, nil
}
func (h *testHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	h.identities++
	return h.identity, nil
}
func (h *testHost) Runtime(context.Context, RuntimeRequirement) (RuntimeInspection, error) {
	h.runtimes++
	return h.runtime, nil
}

type testCatalog struct {
	definition Definition
	err        error
	admitted   []NativeRequirements
}

func (c *testCatalog) ValidateEgress(SetupEgress) error { return nil }

func (c *testCatalog) Select(_ Platform, requirements NativeRequirements) (Definition, error) {
	c.admitted = append(c.admitted, requirements)
	return c.definition, c.err
}

type testBundle struct {
	owner                 *fixture
	ready, recoverable    bool
	toolsReady            bool
	sealed                bool
	prepares, inspections int
	err                   error
	cancel                context.CancelFunc
}

func (b *testBundle) Inspect(context.Context, BundleArea, Definition, bool) (BundleInspection, error) {
	b.inspections++
	return BundleInspection{Ready: b.ready, ToolsReady: b.toolsReady, Sealed: b.sealed, Recoverable: b.recoverable}, nil
}
func (b *testBundle) Prepare(ctx context.Context, _ BundleArea, _ Definition, _ SetupEgress, _ func(ProgressEvent)) error {
	b.prepares++
	b.owner.events = append(b.owner.events, "prepare")
	if b.owner.store.state.Receipt.Actions[0].Phase != "intent" {
		return errors.New("installation without recorded intent")
	}
	if b.cancel != nil {
		b.cancel()
		return ctx.Err()
	}
	if b.err != nil {
		return b.err
	}
	b.ready = true
	return nil
}

type testCompiler struct {
	calls        int
	runtime      bool
	managedProxy bool
	machine      string
	extra        []api.Object
}

func (c *testCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	c.calls++
	name := c.machine
	if name == "" {
		name = "bastion"
	}
	environment := api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue(name), "controller", "machineRef"))
	spec := api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct")
	if c.runtime {
		spec = spec.With("capabilities", api.StringList("container-runtime"))
	}
	objects := []api.Object{environment, api.NewObject(api.Machine, name, api.Value{}, spec)}
	objects = append(objects, c.extra...)
	if c.managedProxy {
		objects[1] = objects[1].WithSpec(spec.With("proxy", api.MapValue().With("proxyRef", api.StringValue("egress")).With("endpointRef", api.StringValue("main"))))
		objects = append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue().With("management", api.StringValue("managed"))))
	}
	catalog := api.NewCatalog(objects)
	return compilation.NewState(catalog, catalog, nil), nil, nil
}

func TestBaselineDryRunHasNoPrivateOrEffectCapabilities(t *testing.T) {
	f := newFixture(t)
	result, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
	if err != nil || result == nil || result.Outcome != "planned" || !result.DryRun {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(f.events, []string{"platform"}) || f.host.identities != 0 || f.host.runtimes != 0 || f.compiler.calls != 0 || f.bundle.inspections != 0 || f.store.writes != 0 {
		t.Fatalf("dry-run crossed effect boundary: %#v", f)
	}
	if result.Checks[1].Status != "unverified" {
		t.Fatal("dry-run claimed unobserved readiness")
	}
}

func TestSetupDurableIntentAndReadOnlyNoop(t *testing.T) {
	f := newFixture(t)
	result, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || result.Outcome != "changed" || !result.PlanPresented || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("setup=%#v err=%v state=%#v", result, err, f.store.state)
	}
	want := []string{"read:", "platform", "present", "confirm", "mutate", "platform", "prepare"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("order=%v", f.events)
	}
	writes := f.store.writes
	f.events = nil
	result, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || result.Outcome != "unchanged" || f.store.writes != writes || f.bundle.prepares != 1 || slices.Contains(f.events, "confirm") || slices.Contains(f.events, "mutate") {
		t.Fatalf("no-op=%#v err=%v events=%v", result, err, f.events)
	}
	result, err = f.service.Check(context.Background(), CheckRequest{})
	if err != nil || result.Outcome != "ready" || f.store.writes != writes {
		t.Fatalf("preflight=%#v err=%v", result, err)
	}
}

func TestMissingPreflightDoesNotBootstrap(t *testing.T) {
	f := newFixture(t)
	result, err := f.service.Check(context.Background(), CheckRequest{})
	if result == nil || result.Outcome != "not-ready" || code(err) != "preflight.failed" || f.store.exists || f.store.writes != 0 || f.bundle.prepares != 0 {
		t.Fatalf("preflight=%#v err=%v", result, err)
	}
}

func TestDeclineOutputFailureAndUnsupportedCatalogHaveNoMutation(t *testing.T) {
	for _, stage := range []string{"confirm", "present", "catalog"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			rejected := errors.New("rejected")
			switch stage {
			case "confirm":
				f.confirmationError = rejected
			case "present":
				f.presentationError = rejected
			case "catalog":
				f.catalog.err = rejected
			}
			_, err := f.service.Setup(context.Background(), SetupRequest{})
			if !errors.Is(err, rejected) || f.store.exists || f.store.writes != 0 || f.bundle.prepares != 0 {
				t.Fatalf("effects after %s refusal: %v", stage, err)
			}
		})
	}
}

func TestStateChangeAfterConfirmationRefusesEffects(t *testing.T) {
	f := newFixture(t)
	f.beforeMutation = func() {
		f.host.identity, _ = controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("2", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	}
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.conflict" || f.store.writes != 0 || f.bundle.prepares != 0 {
		t.Fatalf("changed host error=%v", err)
	}
}

func TestPartialBundleRetriesExactIntent(t *testing.T) {
	f := newFixture(t)
	f.bundle.err = errors.New("acquisition failed")
	result, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err == nil || result.Outcome != "incomplete" || f.store.state.Receipt.Actions[0].Phase != "intent" {
		t.Fatalf("partial=%#v err=%v", result, err)
	}
	before := f.store.state.Receipt
	f.bundle.err = nil
	result, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || result.Outcome != "changed" || f.store.state.Receipt.ID != before.ID || f.store.state.Receipt.PlanDigest != before.PlanDigest || f.bundle.prepares != 2 {
		t.Fatalf("retry=%#v err=%v", result, err)
	}
}

func TestPositivePostconditionCompletesLostResultWithoutRepeatedInstall(t *testing.T) {
	f := newFixture(t)
	f.store.failPublication = 3
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || !f.bundle.ready || f.store.state.Receipt.Actions[0].Phase != "intent" {
		t.Fatalf("failed publication=%v", err)
	}
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || f.bundle.prepares != 1 || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("recovery=%v prepares=%d", err, f.bundle.prepares)
	}
}

func TestCancellationRetainsIntentAndStopsActions(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.bundle.cancel = cancel
	result, err := f.service.Setup(ctx, SetupRequest{SkipConfirmation: true})
	if !errors.Is(err, context.Canceled) || result.Outcome != "incomplete" || f.store.state.Receipt.Actions[0].Phase != "intent" || !f.store.state.Receipt.Incomplete() {
		t.Fatalf("cancel=%#v err=%v", result, err)
	}
}

func TestExplicitSelectionBindingAndWrongMachineRefusal(t *testing.T) {
	f := newFixture(t)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	result, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || result.Machine != "bastion" || len(f.store.state.Bindings) != 1 || f.store.state.Receipt.Context.Machine != "bastion" {
		t.Fatalf("binding=%#v err=%v", result, err)
	}
	f.compiler.machine = "replacement"
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.identity" {
		t.Fatalf("rebind error=%v", err)
	}
}

func TestPendingReceiptRejectsChangedCatalogWithoutEffects(t *testing.T) {
	f := newFixture(t)
	f.bundle.err = errors.New("failed")
	_, _ = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	f.catalog.definition.CatalogDigest = strings.Repeat("c", 64)
	writes := f.store.writes
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || f.store.writes != writes || f.bundle.prepares != 1 {
		t.Fatalf("closure substitution=%v", err)
	}
}

func code(err error) string {
	values := diagnostics.Of(err)
	if len(values) == 0 {
		return ""
	}
	return values[0].Code
}

type testRuntimeInstaller struct {
	owner             *fixture
	calls             int
	result            ActionResult
	err               error
	recovers          int
	entered           int
	beforePreparation error
	recoveryError     error
}

func (r *testRuntimeInstaller) Recover(context.Context, BundleArea, Platform, Definition, SetupEgress, NativePreparation, func(ProgressEvent)) (ActionResult, error) {
	r.recovers++
	if r.recoveryError != nil {
		return ActionResult{}, r.recoveryError
	}
	r.owner.bundle.toolsReady = true
	return ActionResult{Outcome: "unchanged", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
}

func (r *testRuntimeInstaller) Prepare(ctx context.Context, area BundleArea, platform Platform, definition Definition, route SetupEgress, record func(context.Context, NativePreparation) error, _ func(ProgressEvent)) (ActionResult, error) {
	r.calls++
	if area == nil || !r.owner.bundle.ready || !slices.ContainsFunc(r.owner.store.state.Receipt.Actions, func(a SetupAction) bool { return a.ID == "container-runtime" && a.Phase == "intent" }) {
		return ActionResult{}, errors.New("runtime without qualified bundle and durable intent")
	}
	if r.result.Outcome != "failed" {
		if r.beforePreparation != nil {
			return ActionResult{}, r.beforePreparation
		}
		preparation := NativePreparation{InventorySHA256: strings.Repeat("d", 64), AddedSources: []string{"python"}}
		if definition.Native != nil {
			// A resolved native plan binds the preparation to its exact frozen
			// transitions, exactly as the real installer reports them.
			transitions, err := NativeTransitionsDigest(definition.Native.Actions)
			if err != nil {
				return ActionResult{}, err
			}
			added := []string{}
			for _, action := range definition.Native.Actions {
				added = append(added, action.SourceID)
			}
			slices.Sort(added)
			preparation = NativePreparation{InventorySHA256: definition.Native.BeforeSHA256, AfterInventorySHA256: definition.Native.AfterSHA256, PlanDigest: definition.Native.Digest, TransitionsSHA256: transitions, AddedSources: added}
		}
		if err := record(ctx, preparation); err != nil {
			return ActionResult{}, err
		}
		r.entered++
	}
	if r.err == nil {
		r.owner.host.runtime = RuntimeInspection{Present: true, Ready: true}
		r.owner.bundle.toolsReady = true
	}
	return r.result, r.err
}

func explicitRuntimeFixture(t *testing.T) (*fixture, *testRuntimeInstaller) {
	t.Helper()
	f := newFixture(t)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	f.compiler.runtime = true
	f.host.runtime = RuntimeInspection{}
	r := &testRuntimeInstaller{owner: f, result: ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}}
	f.service.runtime = r
	return f, r
}

func TestAbsentRuntimeInstallationPrecedesBinding(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	result, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || result.Outcome != "changed" || r.calls != 1 || f.store.state.Receipt.Status != "complete" || len(f.store.state.Bindings) != 1 {
		t.Fatalf("runtime=%#v err=%v calls=%d", result, err, r.calls)
	}
	if string(f.store.state.Receipt.Actions[1].Evidence) != `{"nativePostcondition":"verified"}` {
		t.Fatal("native evidence was discarded")
	}
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example"})
	if err != nil || r.calls != 1 {
		t.Fatalf("runtime no-op repeated install: %v", err)
	}
}

func TestMissingNativeDependencyWithExistingPodmanIsPrepared(t *testing.T) {
	f, installer := explicitRuntimeFixture(t)
	f.host.runtime = RuntimeInspection{Present: true}
	report, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || installer.calls != 1 {
		t.Fatalf("missing native dependency was not prepared: %#v %v", report, err)
	}
}

func TestConflictingNativeDependencyRefusesBeforeMutation(t *testing.T) {
	f, installer := explicitRuntimeFixture(t)
	f.host.runtime = RuntimeInspection{Present: true, Conflict: true}
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unsupported" || installer.calls != 0 || f.store.writes != 0 {
		t.Fatalf("conflicting native dependency was mutated: %v", err)
	}
}

func TestDefiniteNativeRefusalPreservesBundleAndPermitsFreshAttempt(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = failure("controller.unsupported", "provided native foundation is incompatible", "prepare the qualified foundation")
	r.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unsupported" || f.store.state.Receipt.Status != "failed" || !f.bundle.ready || len(f.store.state.Bindings) != 0 {
		t.Fatalf("native refusal=%v state=%#v", err, f.store.state)
	}
	first := f.store.state.Receipt.ID
	r.err = nil
	r.result = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || f.bundle.prepares != 1 || r.calls != 2 || f.store.state.Receipt.ID == first || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("native retry=%v", err)
	}
}

func TestUnknownNativeOutcomeBlocksReinstallUntilPositiveReadiness(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = failure("controller.unknown", "native result was lost", "resolve the native transaction")
	r.result = ActionResult{Outcome: "unknown", Evidence: object(map[string]any{"installationEntered": true})}
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || !f.store.state.Receipt.Incomplete() {
		t.Fatalf("native unknown=%v", err)
	}
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || r.calls != 1 {
		t.Fatalf("repeated unproved native action=%v", err)
	}
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || r.calls != 1 || r.recovers != 1 || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("positive native resolution=%v", err)
	}
}

func TestNativePreparationMustCommitBeforeInstallerEntry(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	// Receipt, bundle intent, bundle observation and runtime intent precede the
	// durable before-inventory publication. An uncertain fifth publication must
	// prevent authorization delivery to the native transaction.
	f.store.failPublication = 5
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || r.calls != 1 || r.entered != 0 || len(f.store.state.Bindings) != 0 {
		t.Fatalf("native entered without durable preparation: err=%v entered=%d", err, r.entered)
	}
}

func TestNativeIntentWithoutPreparationCanRetryBeforeInstallation(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.beforePreparation = errors.New("native lock unavailable before authorization")
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err == nil || r.entered != 0 || len(f.store.state.Receipt.Actions[1].Preparation) != 0 {
		t.Fatalf("unexpected preparation: %v", err)
	}
	r.beforePreparation = nil
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || r.calls != 2 || r.entered != 1 || r.recovers != 0 || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("unentered native retry: %v", err)
	}
}

func TestNativeRecoveryRequiresOriginalInventoryProof(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = errors.New("native completion lost")
	r.result.Outcome = "unknown"
	_, _ = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	r.recoveryError = failure("controller.unknown", "original inventory cannot be reconstructed", "restore the exact native evidence")
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || r.calls != 1 || r.recovers != 1 || !f.store.state.Receipt.Incomplete() || len(f.store.state.Bindings) != 0 {
		t.Fatalf("unproved native recovery completed: %v", err)
	}
}

func TestPendingReceiptCannotOmitOrChangeRequiredActions(t *testing.T) {
	for _, change := range []string{"omit", "request"} {
		t.Run(change, func(t *testing.T) {
			f, r := explicitRuntimeFixture(t)
			r.err = errors.New("native result lost")
			r.result = ActionResult{Outcome: "unknown", Evidence: object(map[string]any{"installationEntered": true})}
			_, _ = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
			if change == "omit" {
				f.store.state.Receipt.Actions = f.store.state.Receipt.Actions[:1]
			} else {
				f.store.state.Receipt.Actions[1].Request = object(map[string]any{"readyBefore": false, "version": "unapproved"})
			}
			writes := f.store.writes
			_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
			if code(err) != "controller.unknown" || f.store.writes != writes || len(f.store.state.Bindings) != 0 {
				t.Fatalf("retained action substitution=%v", err)
			}
		})
	}
}

// Progress must come from the durable receipt, so an interrupted setup can say
// which approved actions took effect.
func TestInterruptedSetupReportsReceiptProgress(t *testing.T) {
	f := newFixture(t)
	f.bundle.err = errors.New("synthetic bundle failure")
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err == nil || report == nil || report.Outcome != "incomplete" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if len(report.Progress) == 0 {
		t.Fatal("interrupted setup reported no per-action progress")
	}
	for _, action := range report.Progress {
		if action.ID == "execution-bundle" && action.Phase == "observed" {
			t.Fatal("a failed action was reported as observed")
		}
	}
	if report.Progress[0].ID != "execution-bundle" {
		t.Fatalf("progress is not in receipt order: %#v", report.Progress)
	}
}

// A refusal raised before any durable intent changed nothing, so it must not
// report the incomplete setup that recovery guidance is written for.
func TestRefusalBeforeDurableIntentIsNotReportedAsIncomplete(t *testing.T) {
	f := newFixture(t)
	f.store.mutationError = errors.New("synthetic coordination refusal")
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err == nil || report == nil {
		t.Fatalf("refusal accepted: %#v %v", report, err)
	}
	if len(report.Progress) != 0 {
		t.Fatalf("a refusal before durable intent reported progress: %#v", report.Progress)
	}
	if report.Outcome != "planned" {
		t.Fatalf("outcome = %q, want planned", report.Outcome)
	}
}
