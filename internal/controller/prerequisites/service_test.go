package prerequisites

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	plan              Report
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

func (f *fixture) PresentControllerPlan(ctx context.Context, report Report) error {
	f.events = append(f.events, "present")
	f.plan = report
	return f.presentationError
}

type memoryStorage struct {
	owner *fixture
	state HostState
	scope SetupContext
	// held is the scope the open mutation was entered with, so a snapshot taken
	// inside it sees exactly the context the caller asked to mutate.
	held            SetupContext
	reads, writes   int
	exists          bool
	bundleExists    bool
	hidden          string
	bundleRequests  []string
	failPublication int
	mutationError   error
	retired         []string
	retireErr       error
	// retiredResolutions is every resolution a retirement dropped alone, in
	// the order it was asked for.
	retiredResolutions []string
	// areas is every bundle area this double holds. Bundle refuses a new one
	// once MaxRetainedBundles are held, as the store does, and interrupted
	// makes the next retirement stop once its intent is recorded.
	areas       []HeldArea
	interrupted bool
	// uncertain is set by a publication whose outcome is unknown and ends
	// every later publication of the same mutation, as the store's does.
	uncertain bool
	// runs are the setup runs this double keeps, oldest first. runFault
	// refuses the next opening, and brokenRuns makes every write and close of
	// an opened run fail.
	runs       []*memoryRun
	runFault   error
	brokenRuns bool
}

// memoryRun is one setup run this double keeps.
type memoryRun struct {
	number int
	output bytes.Buffer
	closed bool
	broken bool
}

func (r *memoryRun) Write(value []byte) (int, error) {
	if r.broken {
		return 0, errors.New("no space left on the device")
	}
	return r.output.Write(value)
}

func (r *memoryRun) Location() string {
	return "/var/lib/bootwright/controller/runs/setup-" + fmt.Sprintf("%06d", r.number)
}

func (r *memoryRun) Close() error {
	r.closed = true
	if r.broken {
		return errors.New("setup run output durability could not be established")
	}
	return nil
}

// OpenRun opens a run only under a durable intent and keeps the newest
// MaxSetupRuns, numbering them upward, as the store does.
func (m *memoryStorage) OpenRun(context.Context) (SetupRun, error) {
	if m.uncertain {
		return nil, errors.New("controller storage capability is no longer available")
	}
	if m.runFault != nil {
		return nil, m.runFault
	}
	if !m.state.Receipt.Incomplete() || !slices.ContainsFunc(m.state.Receipt.Actions, func(a SetupAction) bool { return a.Phase == "intent" }) {
		return nil, errors.New("a setup run requires a durably intended setup action")
	}
	number := 1
	if len(m.runs) != 0 {
		number = m.runs[len(m.runs)-1].number + 1
	}
	for len(m.runs) >= memoryRetainedRuns {
		m.runs = m.runs[1:]
	}
	run := &memoryRun{number: number, broken: m.brokenRuns}
	m.runs = append(m.runs, run)
	return run, nil
}

// memoryRetainedRuns is the store's retention bound for setup runs.
const memoryRetainedRuns = 8

func (m *memoryStorage) view() StorageView {
	value := StorageView{Exists: m.exists, Initialized: m.state.Host.Valid(), Context: m.scope, State: copyState(m.state), Areas: slices.Clone(m.areas)}
	// The store admits only a resolved bundle identity, so this double refuses
	// anything else exactly as the durable adapter does.
	value.OpenBundle = func(_ context.Context, id string) (BundleArea, error) {
		m.bundleRequests = append(m.bundleRequests, id)
		if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 32 {
			return nil, errors.New("controller bundle identity is invalid")
		}
		// hidden is an area this host no longer has, however it was reserved,
		// and a retired one is gone; one being retired is never read.
		if id == m.hidden || slices.Contains(m.retired, id) {
			return nil, nil
		}
		if slices.ContainsFunc(m.areas, func(held HeldArea) bool { return held.ID == id && held.Retiring }) {
			return nil, errors.New("this controller bundle is being retired")
		}
		if m.bundleExists {
			return dummyArea{}, nil
		}
		return nil, nil
	}
	return value
}
func (m *memoryStorage) ReadController(ctx context.Context, name string, fn func(StorageView) error) error {
	if fn == nil {
		return errors.New("controller inspection callback is missing")
	}
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
	if fn == nil {
		return errors.New("controller mutation callback is missing")
	}
	m.owner.events = append(m.owner.events, "mutate")
	if m.owner.beforeMutation != nil {
		m.owner.beforeMutation()
	}
	m.exists = true
	if m.mutationError != nil {
		return m.mutationError
	}
	previous := m.held
	m.held, m.uncertain = scope, false
	defer func() { m.held, m.uncertain = previous, false }()
	return fn(m)
}

func (m *memoryStorage) Snapshot() StorageView {
	value := m.view()
	value.Context = m.held
	return value
}
func (m *memoryStorage) Publish(ctx context.Context, state HostState) (Publication, error) {
	if m.uncertain {
		return NotCommitted, errors.New("controller storage capability is no longer available")
	}
	if err := m.transition(state); err != nil {
		return NotCommitted, err
	}
	retained, err := m.retain(state)
	if err != nil {
		return NotCommitted, err
	}
	m.writes++
	if m.writes == m.failPublication {
		m.uncertain = true
		return Unknown, errors.New("private sync error")
	}
	m.state = copyState(state)
	m.state.RetainedSources = slices.Clone(state.Receipt.Sources)
	m.state.RetainedDefinitions = retained
	if state.Receipt.Status == "complete" {
		m.owner.bundle.sealed = true
	}
	return Committed, nil
}

// retain is what the store keeps of the retained resolutions when it publishes
// next: the ones it already holds, whatever the caller passed, and the one the
// receipt carries. Like the store it refuses a resolution beyond the bound,
// and a new receipt naming a bundle it could not reserve once the areas are at
// the bound.
func (m *memoryStorage) retain(next HostState) ([]Definition, error) {
	retained := slices.Clone(m.state.RetainedDefinitions)
	if definition := next.Receipt.Definition; definition != nil &&
		!slices.ContainsFunc(retained, func(item Definition) bool { return item.ResolutionDigest == definition.ResolutionDigest }) {
		if len(retained) >= MaxRetainedBundles {
			return nil, errors.New("retained controller resolution limit exceeded")
		}
		retained = append(retained, CloneDefinition(*definition))
	}
	if next.Receipt.ID != m.state.Receipt.ID && len(m.areas) >= MaxRetainedBundles &&
		!slices.ContainsFunc(m.areas, func(held HeldArea) bool { return held.ID == next.Receipt.CatalogDigest }) {
		return nil, errors.New("retained controller bundle limit exceeded")
	}
	return retained, nil
}

// transition refuses what the store's receipt rules refuse: a receipt of
// another scope or host, a new receipt over a pending one, and, for the same
// receipt, another plan, a completed receipt made pending, an intent or an
// unresolved effect presumed not to have happened, and a verified postcondition
// replaced.
func (m *memoryStorage) transition(next HostState) error {
	before := m.state
	if next.Receipt.Context != m.held {
		return errors.New("setup receipt differs from its held context scope")
	}
	switch {
	case !before.Host.Valid():
		return nil
	case !before.Host.Equal(next.Host):
		return errors.New("stored controller host evidence does not match the executing host")
	case before.Receipt.ID != next.Receipt.ID && before.Receipt.Incomplete():
		return errors.New("pending setup must be resolved before replacing its receipt")
	case before.Receipt.ID != next.Receipt.ID:
		return nil
	case before.Receipt.PlanDigest != next.Receipt.PlanDigest || len(before.Receipt.Actions) != len(next.Receipt.Actions):
		return errors.New("exact setup retry requires the original host, input, catalog and plan")
	case before.Receipt.Status == "complete" && next.Receipt.Status != "complete":
		return errors.New("completed setup cannot become pending again")
	}
	for index, previous := range before.Receipt.Actions {
		now := next.Receipt.Actions[index]
		if previous.Phase == "intent" && now.Phase == "planned" || previous.Phase == "observed" && previous.Outcome == "unknown" && now.Phase != "observed" {
			return errors.New("setup retry cannot presume an unresolved effect did not occur")
		}
		if previous.Phase == "observed" && (previous.Outcome == "changed" || previous.Outcome == "unchanged") &&
			(now.Phase != previous.Phase || now.Outcome != previous.Outcome || !bytes.Equal(now.Evidence, previous.Evidence)) {
			return errors.New("setup cannot discard a verified action postcondition")
		}
	}
	return nil
}

// Bundle opens only the approved catalog's bundle, and only under a durable
// intent, as the store does.
func (m *memoryStorage) Bundle(ctx context.Context, id string) (BundleArea, error) {
	if m.uncertain {
		return nil, errors.New("controller storage capability is no longer available")
	}
	if id != m.state.Receipt.CatalogDigest || id == "" {
		return nil, errors.New("bundle namespace is not the approved setup catalog")
	}
	if !slices.ContainsFunc(m.state.Receipt.Actions, func(a SetupAction) bool { return a.Phase == "intent" }) {
		return nil, errors.New("missing durable intent")
	}
	if !slices.ContainsFunc(m.areas, func(held HeldArea) bool { return held.ID == id }) {
		if len(m.areas) >= MaxRetainedBundles {
			return nil, errors.New("retained controller bundle limit exceeded")
		}
		m.areas = append(m.areas, HeldArea{ID: id})
		slices.SortFunc(m.areas, func(a, b HeldArea) int { return strings.Compare(a.ID, b.ID) })
	}
	m.bundleExists = true
	return dummyArea{}, nil
}

// RetireBundles records what a retirement asked for and drops the retained
// resolutions it names, exactly as the store does, and refuses what the store
// refuses: any retirement without a receipt or while it is pending, an
// identity that is no digest and the bundle the receipt names.
func (m *memoryStorage) RetireBundles(_ context.Context, ids []string) error {
	if m.retireErr != nil {
		return m.retireErr
	}
	if m.uncertain {
		return errors.New("controller storage capability is no longer available")
	}
	if m.state.Receipt.ID == "" || m.state.Receipt.Incomplete() {
		return errors.New("controller retirement requires a settled setup receipt")
	}
	for _, id := range ids {
		if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 32 {
			return errors.New("retired controller bundle identity is invalid")
		}
	}
	if slices.Contains(ids, m.state.Receipt.CatalogDigest) {
		return errors.New("the execution bundle this receipt names may not be retired")
	}
	m.state.RetainedDefinitions = slices.DeleteFunc(slices.Clone(m.state.RetainedDefinitions),
		func(definition Definition) bool { return slices.Contains(ids, definition.CatalogDigest) })
	if m.interrupted {
		m.interrupted = false
		for index := range m.areas {
			m.areas[index].Retiring = m.areas[index].Retiring || slices.Contains(ids, m.areas[index].ID)
		}
		return errors.New("synthetic process death after the retirement intent")
	}
	m.retired = append(m.retired, ids...)
	m.areas = slices.DeleteFunc(m.areas, func(held HeldArea) bool { return slices.Contains(ids, held.ID) })
	return nil
}

// RetireResolutions drops the retained resolutions it names, exactly as the
// store does, and refuses what the store refuses: any retirement without a
// receipt or while it is pending, an identity that is no digest, the
// resolution the receipt carries and one whose bundle no other retained
// resolution would still name.
func (m *memoryStorage) RetireResolutions(_ context.Context, digests []string) error {
	if m.retireErr != nil {
		return m.retireErr
	}
	if m.uncertain {
		return errors.New("controller storage capability is no longer available")
	}
	if m.state.Receipt.ID == "" || m.state.Receipt.Incomplete() {
		return errors.New("controller retirement requires a settled setup receipt")
	}
	for _, digest := range digests {
		if decoded, err := hex.DecodeString(digest); err != nil || len(decoded) != 32 {
			return errors.New("retired controller resolution identity is invalid")
		}
		if m.state.Receipt.Definition != nil && digest == m.state.Receipt.Definition.ResolutionDigest {
			return errors.New("the resolution this receipt carries may not be retired")
		}
	}
	retiring := func(definition Definition) bool { return slices.Contains(digests, definition.ResolutionDigest) }
	kept := slices.DeleteFunc(slices.Clone(m.state.RetainedDefinitions), retiring)
	var retired []string
	for _, digest := range digests {
		index := slices.IndexFunc(m.state.RetainedDefinitions, func(definition Definition) bool { return definition.ResolutionDigest == digest })
		if index < 0 || slices.Contains(retired, digest) {
			continue
		}
		bundle := m.state.RetainedDefinitions[index].CatalogDigest
		if !slices.ContainsFunc(kept, func(other Definition) bool { return other.CatalogDigest == bundle }) {
			return errors.New("a retained resolution may not be retired while no other names its bundle")
		}
		retired = append(retired, digest)
	}
	m.retiredResolutions = append(m.retiredResolutions, retired...)
	m.state.RetainedDefinitions = kept
	return nil
}

// copyState copies every mutable value a HostState reaches, as the store
// copies what crosses its boundary.
func copyState(s HostState) HostState {
	s.Bindings = slices.Clone(s.Bindings)
	s.Reservations = slices.Clone(s.Reservations)
	for index := range s.Reservations {
		s.Reservations[index].Keys = slices.Clone(s.Reservations[index].Keys)
	}
	s.RetainedSources = slices.Clone(s.RetainedSources)
	s.RetainedDefinitions = slices.Clone(s.RetainedDefinitions)
	for index := range s.RetainedDefinitions {
		s.RetainedDefinitions[index] = CloneDefinition(s.RetainedDefinitions[index])
	}
	if s.Receipt.Definition != nil {
		definition := CloneDefinition(*s.Receipt.Definition)
		s.Receipt.Definition = &definition
	}
	s.Receipt.Actions = slices.Clone(s.Receipt.Actions)
	for index := range s.Receipt.Actions {
		s.Receipt.Actions[index].Request = slices.Clone(s.Receipt.Actions[index].Request)
		s.Receipt.Actions[index].Evidence = slices.Clone(s.Receipt.Actions[index].Evidence)
		s.Receipt.Actions[index].Preparation = slices.Clone(s.Receipt.Actions[index].Preparation)
	}
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
	// automation is the digest a rebase stamps, rebaseErr its refusal, and
	// retainedSeeds records for each preparation whether it was offered the
	// sealed area a carried resolution came from.
	rebases       int
	automation    string
	rebaseErr     error
	retainedSeeds []bool
	egress        SetupEgress
}

func (b *testBundle) Inspect(context.Context, BundleArea, Definition, bool) (BundleInspection, error) {
	b.inspections++
	return BundleInspection{Ready: b.ready, ToolsReady: b.toolsReady, Sealed: b.sealed, Recoverable: b.recoverable}, nil
}

// Rebase reprojects the retained resolution under a different automation, which
// is the whole observable effect: the returned bootstrap names the same
// releases and sources and only its digest moves.
func (b *testBundle) Rebase(_ context.Context, area BundleArea, retained BootstrapDefinition) (BootstrapDefinition, error) {
	b.rebases++
	b.owner.events = append(b.owner.events, "rebase")
	if area == nil {
		return BootstrapDefinition{}, errors.New("rebase without the retained bundle to read")
	}
	if b.rebaseErr != nil {
		return BootstrapDefinition{}, b.rebaseErr
	}
	value := retained
	value.AutomationDigest = b.automation
	return CanonicalBootstrap(value)
}

func (b *testBundle) Prepare(ctx context.Context, _ BundleArea, retained BundleArea, _ Definition, egress SetupEgress, _ func(ProgressEvent)) (BundleInspection, error) {
	b.prepares++
	b.egress = egress
	b.retainedSeeds = append(b.retainedSeeds, retained != nil)
	b.owner.events = append(b.owner.events, "prepare")
	if b.owner.store.state.Receipt.Actions[0].Phase != "intent" {
		return BundleInspection{}, errors.New("installation without recorded intent")
	}
	if b.cancel != nil {
		b.cancel()
		return BundleInspection{}, ctx.Err()
	}
	if b.err != nil {
		return BundleInspection{}, b.err
	}
	b.ready = true
	return BundleInspection{Ready: true, ToolsReady: b.toolsReady, Sealed: b.sealed, Recoverable: b.recoverable}, nil
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
		name = "controller"
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

// Setup claims no context. It publishes no binding, records no controller
// Machine on its receipt and never compiles desired state, so a host prepared
// once serves every context that is later created on it.
func TestSetupClaimsNoContextAndPublishesNoBinding(t *testing.T) {
	f := newFixture(t)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	result, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || result.Outcome != "changed" {
		t.Fatalf("setup=%#v err=%v", result, err)
	}
	if result.ContextName != "" || result.Machine != "" || len(f.store.state.Bindings) != 0 {
		t.Fatalf("setup claimed a context: report=%#v bindings=%#v", result, f.store.state.Bindings)
	}
	if f.store.state.Receipt.Context != (SetupContext{}) || f.compiler.calls != 0 {
		t.Fatalf("setup read desired state: receipt=%#v compilations=%d", f.store.state.Receipt.Context, f.compiler.calls)
	}
	// A different controller Machine changes nothing setup owns.
	f.compiler.machine = "replacement"
	result, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || result.Outcome != "unchanged" {
		t.Fatalf("a changed controller Machine disturbed setup: %#v %v", result, err)
	}
}

// Preflight still proves the binding a context's first apply published, and
// refuses when that binding names another controller Machine.
func TestContextPreflightVerifiesTheEstablishedBinding(t *testing.T) {
	f := newFixture(t)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	digest, err := f.host.identity.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	f.store.state.Bindings = []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if err != nil || report.Outcome != "ready" {
		t.Fatalf("bound context was not ready: %#v %v", report, err)
	}
	f.compiler.machine = "replacement"
	if _, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"}); code(err) != "controller.identity" {
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
	// outputs is what each run was given to keep its output in, and started
	// runs as each Ansible run starts, before it prints anything.
	outputs []RunOutput
	started func()
}

// print is what the Ansible this double stands for writes, as a real run
// writes its callback lines.
func (r *testRuntimeInstaller) print(output RunOutput, line string) {
	r.outputs = append(r.outputs, output)
	if r.started != nil {
		r.started()
	}
	if output != nil {
		_, _ = output.Write([]byte(line))
	}
}

func (r *testRuntimeInstaller) Recover(_ context.Context, _ BundleArea, _ Platform, _ Definition, _ SetupEgress, _ NativePreparation, _ func(ProgressEvent), output RunOutput) (ActionResult, error) {
	r.recovers++
	r.print(output, "TASK [recover the recorded native transaction]\n")
	if r.recoveryError != nil {
		return ActionResult{}, r.recoveryError
	}
	r.owner.bundle.toolsReady = true
	return ActionResult{Outcome: "unchanged", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
}

func (r *testRuntimeInstaller) Prepare(ctx context.Context, area BundleArea, platform Platform, definition Definition, route SetupEgress, record func(context.Context, NativePreparation) error, _ func(ProgressEvent), output RunOutput) (ActionResult, error) {
	r.calls++
	r.print(output, "TASK [install the native packages]\n")
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

func TestAbsentRuntimeIsInstalledAndRecorded(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	result, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || result.Outcome != "changed" || r.calls != 1 || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("runtime=%#v err=%v calls=%d", result, err, r.calls)
	}
	if string(f.store.state.Receipt.Actions[1].Evidence) != `{"nativePostcondition":"verified"}` {
		t.Fatal("native evidence was discarded")
	}
	_, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || r.calls != 1 {
		t.Fatalf("runtime no-op repeated install: %v", err)
	}
}

func TestMissingNativeDependencyWithExistingPodmanIsPrepared(t *testing.T) {
	f, installer := explicitRuntimeFixture(t)
	f.host.runtime = RuntimeInspection{Present: true}
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || installer.calls != 1 {
		t.Fatalf("missing native dependency was not prepared: %#v %v", report, err)
	}
}

func TestConflictingNativeDependencyRefusesBeforeMutation(t *testing.T) {
	f, installer := explicitRuntimeFixture(t)
	f.host.runtime = RuntimeInspection{Present: true, Conflict: true}
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unsupported" || installer.calls != 0 || f.store.writes != 0 {
		t.Fatalf("conflicting native dependency was mutated: %v", err)
	}
}

func TestDefiniteNativeRefusalPreservesBundleAndPermitsFreshAttempt(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = failure("controller.unsupported", "provided native foundation is incompatible", "prepare the qualified foundation")
	r.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unsupported" || f.store.state.Receipt.Status != "failed" || !f.bundle.ready || len(f.store.state.Bindings) != 0 {
		t.Fatalf("native refusal=%v state=%#v", err, f.store.state)
	}
	first := f.store.state.Receipt.ID
	r.err = nil
	r.result = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || f.bundle.prepares != 1 || r.calls != 2 || f.store.state.Receipt.ID == first || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("native retry=%v", err)
	}
}

func TestUnknownNativeOutcomeBlocksReinstallUntilPositiveReadiness(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = failure("controller.unknown", "native result was lost", "resolve the native transaction")
	r.result = ActionResult{Outcome: "unknown", Evidence: object(map[string]any{"installationEntered": true})}
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || !f.store.state.Receipt.Incomplete() {
		t.Fatalf("native unknown=%v", err)
	}
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || r.calls != 1 {
		t.Fatalf("repeated unproved native action=%v", err)
	}
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
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
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || r.calls != 1 || r.entered != 0 || len(f.store.state.Bindings) != 0 {
		t.Fatalf("native entered without durable preparation: err=%v entered=%d", err, r.entered)
	}
}

func TestNativeIntentWithoutPreparationCanRetryBeforeInstallation(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.beforePreparation = errors.New("native lock unavailable before authorization")
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err == nil || r.entered != 0 || len(f.store.state.Receipt.Actions[1].Preparation) != 0 {
		t.Fatalf("unexpected preparation: %v", err)
	}
	r.beforePreparation = nil
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || r.calls != 2 || r.entered != 1 || r.recovers != 0 || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("unentered native retry: %v", err)
	}
}

func TestNativeRecoveryRequiresOriginalInventoryProof(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	r.err = errors.New("native completion lost")
	r.result.Outcome = "unknown"
	_, _ = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	r.recoveryError = failure("controller.unknown", "original inventory cannot be reconstructed", "restore the exact native evidence")
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
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
			_, _ = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if change == "omit" {
				f.store.state.Receipt.Actions = f.store.state.Receipt.Actions[:1]
			} else {
				f.store.state.Receipt.Actions[1].Request = object(map[string]any{"readyBefore": false, "version": "unapproved"})
			}
			writes := f.store.writes
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
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

// Retirement removes the execution bundles a completed setup no longer needs,
// and nothing else. What the receipt names is never one of them, and a setup
// that did not complete retires nothing at all.
func TestPurgeRetiresOnlySupersededBundlesOfACompletedSetup(t *testing.T) {
	f := newFixture(t)
	result, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || result.Outcome != "changed" {
		t.Fatalf("setup=%#v err=%v", result, err)
	}
	current := f.store.state.Receipt.CatalogDigest
	superseded := strings.Repeat("b", 64)
	f.store.state.RetainedDefinitions = []Definition{
		{CatalogDigest: superseded}, {CatalogDigest: current},
	}
	result, err = f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
	if err != nil || result.Outcome != "unchanged" {
		t.Fatalf("purge=%#v err=%v", result, err)
	}
	if !slices.Equal(result.RetiredBundles, []string{superseded}) {
		t.Fatalf("retired = %v", result.RetiredBundles)
	}
	if !slices.Equal(f.store.retired, []string{superseded}) {
		t.Fatalf("the store was asked to retire %v", f.store.retired)
	}
	// The resolution a retired bundle carries goes with it; the one the receipt
	// names stays, because the next carry-forward reads it.
	if len(f.store.state.RetainedDefinitions) != 1 || f.store.state.RetainedDefinitions[0].CatalogDigest != current {
		t.Fatalf("retained definitions = %#v", f.store.state.RetainedDefinitions)
	}
	// Repeating it retires nothing, because nothing is superseded any more.
	f.store.retired = nil
	result, err = f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
	if err != nil || len(result.RetiredBundles) != 0 || len(f.store.retired) != 0 {
		t.Fatalf("a second purge retired %v (%v)", f.store.retired, err)
	}
}

// A retirement interrupted once its intent is recorded has already dropped the
// resolution that named its area, so one repeat of the purge finishes it from
// the mark alone and leaves no area retiring. A client area, which no
// resolution names and nothing marks as retiring, stays untouched throughout.
func TestRepeatingThePurgeCompletesAnInterruptedRetirement(t *testing.T) {
	f := newFixture(t)
	if result, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil || result.Outcome != "changed" {
		t.Fatalf("setup=%#v err=%v", result, err)
	}
	current := f.store.state.Receipt.CatalogDigest
	superseded := supersede(f, 1)
	client := HeldArea{ID: strings.Repeat("c", 64)}
	f.store.areas = append(f.store.areas, client)
	slices.SortFunc(f.store.areas, func(a, b HeldArea) int { return strings.Compare(a.ID, b.ID) })
	f.store.interrupted = true
	if _, err := f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true}); err == nil {
		t.Fatal("the interruption was not reported")
	}
	if f.store.interrupted || !slices.Contains(f.store.areas, HeldArea{ID: superseded[0], Retiring: true}) ||
		slices.ContainsFunc(f.store.state.RetainedDefinitions, func(definition Definition) bool { return definition.CatalogDigest == superseded[0] }) {
		t.Fatalf("the interruption left areas %#v and resolutions %#v", f.store.areas, f.store.state.RetainedDefinitions)
	}
	result, err := f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
	if err != nil || result.Outcome != "unchanged" {
		t.Fatalf("repeat=%#v err=%v", result, err)
	}
	if slices.ContainsFunc(f.store.areas, func(held HeldArea) bool { return held.Retiring }) {
		t.Fatalf("the repeat left an area retiring: %#v", f.store.areas)
	}
	if !slices.Equal(f.store.retired, superseded) || !slices.Equal(result.RetiredBundles, superseded) {
		t.Fatalf("retired %v, reported %v, want %v", f.store.retired, result.RetiredBundles, superseded)
	}
	if !holdsReadable(f, current) {
		t.Fatalf("the bundle the receipt names is no longer readable: %#v", f.store.areas)
	}
	if !slices.Contains(f.store.areas, client) || slices.Contains(f.store.retired, client.ID) || slices.Contains(result.RetiredBundles, client.ID) {
		t.Fatalf("the purge touched a client area: areas %#v, retired %v, reported %v", f.store.areas, f.store.retired, result.RetiredBundles)
	}
}

// Retirement is decided by what a completed setup left behind, so a preview and
// a refusal retire nothing.
func TestPurgeRetiresNothingWithoutACompletedSetup(t *testing.T) {
	for name, request := range map[string]SetupRequest{
		"dry run":  {DryRun: true, PurgeOldBundles: true},
		"declined": {PurgeOldBundles: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if name == "declined" {
				f.confirmationError = errors.New("declined")
			}
			f.store.state.RetainedDefinitions = []Definition{{CatalogDigest: strings.Repeat("b", 64)}}
			result, _ := f.service.Setup(context.Background(), request)
			if result != nil && len(result.RetiredBundles) != 0 || len(f.store.retired) != 0 {
				t.Fatalf("retired %v without a completed setup", f.store.retired)
			}
		})
	}
}

// A retirement that cannot be completed fails the invocation rather than
// reporting a setup that quietly left the host at its retention bound.
func TestAFailedRetirementIsReported(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	f.store.state.RetainedDefinitions = []Definition{{CatalogDigest: strings.Repeat("b", 64)}}
	f.store.retireErr = errors.New("busy")
	result, err := f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
	if err == nil {
		t.Fatal("a failed retirement was reported as success")
	}
	if result == nil || len(result.RetiredBundles) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func testRoute(t *testing.T, pairs ...string) controller.Route {
	t.Helper()
	values := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		values[pairs[index]] = pairs[index+1]
	}
	route, err := controller.RouteFromEnvironment(func(name string) (string, bool) {
		value, present := values[name]
		return value, present
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func ambientFixture(t *testing.T, pairs ...string) *fixture {
	t.Helper()
	f := newFixture(t)
	f.service = New(&f.store, &f.compiler, &f.host, &f.catalog, &f.bundle, nil, Options{Confirmer: f, Presenter: f, AmbientRoute: testRoute(t, pairs...)})
	return f
}

// Setup prepares what every context shares, so the only place its proxy choice
// can come from is the environment that invoked it.
func TestContextFreeSetupAcquiresOverTheAmbientRoute(t *testing.T) {
	f := ambientFixture(t, "HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example")
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if report.Route != "http://proxy.example:3128 (HTTPS_PROXY), 1 bypass entry" {
		t.Fatalf("route = %q", report.Route)
	}
	egress := f.store.state.Receipt.Egress
	if egress.HTTPSProxy != "http://proxy.example:3128" || egress.HTTPProxy != "" {
		t.Fatalf("receipt egress = %#v", egress)
	}
	if len(egress.NoProxy) != 1 || egress.NoProxy[0] != ".internal.example" {
		t.Fatalf("receipt bypass = %#v", egress.NoProxy)
	}
	if f.bundle.egress.HTTPSProxy != "http://proxy.example:3128" {
		t.Fatalf("acquisition egress = %#v", f.bundle.egress)
	}
}

func TestAnUnsetEnvironmentKeepsDirectContextFreeAcquisition(t *testing.T) {
	f := ambientFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Route != "direct" {
		t.Fatalf("route = %q err = %v", report.Route, err)
	}
	if f.store.state.Receipt.Egress.HTTPSProxy != "" || len(f.store.state.Receipt.Egress.NoProxy) != 0 {
		t.Fatalf("a direct setup recorded a proxy: %#v", f.store.state.Receipt.Egress)
	}
}

// A context names its own controller Machine, and that Machine's proxy choice
// is the whole route. An environment variable must never reach past it.
func TestASelectedContextIgnoresTheAmbientRoute(t *testing.T) {
	f := ambientFixture(t, "HTTPS_PROXY", "http://proxy.example:3128")
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	digest, err := f.host.identity.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	f.store.state.Bindings = []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Route != "direct" {
		t.Fatalf("a context took the invoking environment's route: %q", report.Route)
	}
}

func TestAnAmbientRouteNeverOverridesASelectedControllerMachine(t *testing.T) {
	route, err := controller.RouteFromEnvironment(func(name string) (string, bool) {
		if name == "HTTPS_PROXY" {
			return "http://proxy.example:3128", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := controller.Baseline().WithAmbientRoute(route)
	if baseline.Route().HTTPSProxy() != "http://proxy.example:3128" {
		t.Fatal("the context-free baseline refused an ambient route")
	}
	selected := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").With("capabilities", api.StringList("container-runtime")).WithPath(api.MapValue(), "proxy", "direct")),
	})
	chosen, err := controller.Select(selected)
	if err != nil {
		t.Fatal(err)
	}
	if overridden := chosen.WithAmbientRoute(route); !overridden.Route().Direct() || overridden.Route().HTTPSProxy() != "" {
		t.Fatal("an ambient route overrode a controller Machine's own proxy choice")
	}
}

// The receipt binds the route its plan was approved with, so an interrupted
// setup cannot be finished over a different one by changing the environment.
func TestAnInterruptedSetupRefusesAChangedAmbientRoute(t *testing.T) {
	for _, test := range []struct {
		name    string
		resumed []string
		code    string
	}{
		{"same route resumes", []string{"HTTPS_PROXY", "http://proxy.example:3128"}, ""},
		{"changed endpoint refuses", []string{"HTTPS_PROXY", "http://other.example:3128"}, "controller.unknown"},
		{"withdrawn proxy refuses", nil, "controller.unknown"},
		{"added bypass refuses", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example"}, "controller.unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := ambientFixture(t, "HTTPS_PROXY", "http://proxy.example:3128")
			f.bundle.err = errors.New("failed")
			if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
				t.Fatal("the interrupted setup reported success")
			}
			if !f.store.state.Receipt.Incomplete() {
				t.Fatal("the interrupted setup left no receipt to protect")
			}
			f.bundle.err = nil
			f.service = New(&f.store, &f.compiler, &f.host, &f.catalog, &f.bundle, nil, Options{Confirmer: f, Presenter: f, AmbientRoute: testRoute(t, test.resumed...)})
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if code(err) != test.code {
				t.Fatalf("resumed under %v: %v", test.resumed, err)
			}
		})
	}
}
