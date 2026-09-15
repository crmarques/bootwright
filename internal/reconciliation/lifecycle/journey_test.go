package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	testContextName = "lab"
	testRevision    = "rev-0123456789abcdef0123456789abcdef"
	testAutomaton   = "aaaa0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
)

// memoryArea models the Workspace-held operation area contract.
type memoryArea struct {
	files map[string][]byte
	fail  map[string]error
}

func newArea() *memoryArea { return &memoryArea{files: map[string][]byte{}, fail: map[string]error{}} }

func (a *memoryArea) clone() *memoryArea {
	copied := newArea()
	for name, data := range a.files {
		copied.files[name] = slices.Clone(data)
	}
	return copied
}

func (a *memoryArea) Read(_ context.Context, target string, _ int) ([]byte, bool, error) {
	if err := a.fail["read "+target]; err != nil {
		return nil, false, err
	}
	data, ok := a.files[target]
	return slices.Clone(data), ok, nil
}

func (a *memoryArea) Entries(_ context.Context, target string) ([]operationstore.Entry, error) {
	prefix := target
	if prefix != "" {
		prefix += "/"
	}
	seen := map[string]operationstore.Entry{}
	for name := range a.files {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		if head, _, nested := strings.Cut(rest, "/"); nested {
			seen[head] = operationstore.Entry{Name: head, Directory: true}
		} else {
			seen[rest] = operationstore.Entry{Name: rest}
		}
	}
	entries := slices.Collect(maps.Values(seen))
	slices.SortFunc(entries, func(x, y operationstore.Entry) int { return strings.Compare(x.Name, y.Name) })
	return entries, nil
}

func (a *memoryArea) EnsureDirectory(context.Context, string) error { return nil }

func (a *memoryArea) WriteExclusive(_ context.Context, target string, data []byte) error {
	if err := a.fail["write "+target]; err != nil {
		return err
	}
	if _, exists := a.files[target]; exists {
		return errors.New("exists")
	}
	a.files[target] = slices.Clone(data)
	return nil
}

func (a *memoryArea) Replace(_ context.Context, target string, data, expected []byte) error {
	if err := a.fail["replace "+target]; err != nil {
		return err
	}
	current, exists := a.files[target]
	if expected == nil {
		if exists {
			return errors.New("exists")
		}
	} else if !exists || !slices.Equal(current, expected) {
		return errors.New("expectation")
	}
	a.files[target] = slices.Clone(data)
	return nil
}

func (a *memoryArea) Append(_ context.Context, target string, data []byte) error {
	if err := a.fail["append "+target]; err != nil {
		return err
	}
	a.files[target] = append(a.files[target], data...)
	return nil
}

func (a *memoryArea) Sync(context.Context, string) error { return nil }

// testWorkspace is one context's durable state: its evidence, reservations and
// operation records, with the same read and mutate boundaries the store has.
type testWorkspace struct {
	area         *memoryArea
	evidence     []byte
	reservations []prerequisites.HostReservation
	controller   prerequisites.StorageView
	inputs       desiredstate.Sources
	mutations    int
	binds        int
	areas        map[string]bool
	retained     []prerequisites.DependencySource
	resolutions  int
	failPublish  error
}

func (w *testWorkspace) view() *testView {
	return &testView{workspace: w}
}

func (w *testWorkspace) ReadLifecycle(ctx context.Context, name string, callback func(View) error) error {
	if name == "" {
		return errors.New("explicit context required")
	}
	return callback(w.view())
}

func (w *testWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	w.mutations++
	return callback(w.view())
}

type testView struct{ workspace *testWorkspace }

func (v *testView) Identity() ContextIdentity {
	return ContextIdentity{Name: testContextName, Revision: testRevision}
}
func (v *testView) Inputs() desiredstate.Sources          { return v.workspace.inputs }
func (v *testView) Controller() prerequisites.StorageView { return v.workspace.controller }
func (v *testView) Evidence() []byte                      { return slices.Clone(v.workspace.evidence) }
func (v *testView) Operations() operationstore.Area       { return v.workspace.area }

func (v *testView) PublishEvidence(_ context.Context, data []byte) error {
	if v.workspace.failPublish != nil {
		return v.workspace.failPublish
	}
	v.workspace.evidence = slices.Clone(data)
	return nil
}

// Bind records the relationship the way the store does: a first apply
// establishes it, a later one revalidates exactly it, and a different Machine
// or host refuses instead of replacing it.
func (v *testView) Bind(_ context.Context, machine string, host controller.InstalledHostIdentity) error {
	digest, err := host.PrivateDigest()
	if err != nil {
		return err
	}
	v.workspace.binds++
	for _, binding := range v.workspace.controller.State.Bindings {
		if binding.Context != v.Identity().Name {
			continue
		}
		if binding.Machine != machine || binding.HostDigest != digest {
			return failure("controller.identity", "this context is already bound to another controller Machine or host", "")
		}
		return nil
	}
	v.workspace.controller.State.Bindings = append(v.workspace.controller.State.Bindings,
		prerequisites.ControllerBinding{Context: v.Identity().Name, Machine: machine, HostDigest: digest})
	return nil
}

func (v *testView) Reserve(_ context.Context, reservations []prerequisites.HostReservation) error {
	v.workspace.reservations = slices.Clone(reservations)
	return nil
}

func (v *testView) ReleaseReservations(context.Context) error {
	v.workspace.reservations = nil
	return nil
}

// ClientArea models the shared host area the controller stage publishes into:
// the reservation is recorded before the area exists, and a sealed closure
// reopens read-only.
func (v *testView) ClientArea(_ context.Context, id string) (prerequisites.BundleArea, error) {
	if v.workspace.areas == nil {
		v.workspace.areas = map[string]bool{}
	}
	if _, exists := v.workspace.areas[id]; !exists {
		v.workspace.areas[id] = false
	}
	return nil, nil
}

func (v *testView) SealClientArea(_ context.Context, id string) error {
	sealed, exists := v.workspace.areas[id]
	if !exists {
		return errors.New("area is not attributed")
	}
	if !sealed {
		v.workspace.areas[id] = true
	}
	return nil
}

func (v *testView) RetainDependencies(_ context.Context, definition *prerequisites.Definition, sources []prerequisites.DependencySource) error {
	v.workspace.retained = append(v.workspace.retained, sources...)
	if definition != nil {
		v.workspace.resolutions++
	}
	return nil
}

// testCapability records what the engine asked it to do and replays scripted
// outcomes, so every orchestration rule is observable without an adapter.
type testCapability struct {
	definitions  []reconciliation.BlockDefinition
	reservations []prerequisites.HostReservation
	secrets      []string
	unsupported  []string
	applies      []string
	destroys     []string
	observes     []string
	outcomes     []Result
	observations []Observation
	planErr      error
	applyErr     error
	material     []map[string]secrets.Material
	executions   []Execution
}

func (c *testCapability) Plan(_ context.Context, input PlanInput) (CapabilityPlan, error) {
	if c.planErr != nil {
		return CapabilityPlan{}, c.planErr
	}
	return CapabilityPlan{Definitions: c.definitions, Reservations: c.reservations, Secrets: c.secrets}, nil
}

func (c *testCapability) Unsupported(*compilation.State) []string { return c.unsupported }

func (c *testCapability) next(outcomes *[]Result) Result {
	if len(*outcomes) == 0 {
		return Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)}
	}
	value := (*outcomes)[0]
	*outcomes = (*outcomes)[1:]
	return value
}

func (c *testCapability) Apply(ctx context.Context, execution Execution) (Result, error) {
	c.applies = append(c.applies, execution.Block.ID)
	c.material = append(c.material, execution.Material)
	c.executions = append(c.executions, execution)
	if execution.Progress != nil {
		execution.Progress(ctx, "pull-image", "running")
		execution.Progress(ctx, "pull-image", "ok")
	}
	if c.applyErr != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, c.applyErr
	}
	result := c.next(&c.outcomes)
	if result.Outcome == reconciliation.OutcomeFailed {
		return result, errors.New("capability failed")
	}
	return result, nil
}

func (c *testCapability) Destroy(_ context.Context, execution Execution) (Result, error) {
	c.destroys = append(c.destroys, execution.Block.ID)
	return c.next(&c.outcomes), nil
}

func (c *testCapability) Observe(_ context.Context, execution Execution) (Observation, error) {
	c.observes = append(c.observes, execution.Block.ID)
	c.executions = append(c.executions, execution)
	if len(c.observations) == 0 {
		return Observation{Effect: reconciliation.EffectUnknown}, nil
	}
	value := c.observations[0]
	c.observations = c.observations[1:]
	return value, nil
}

type testResolver struct {
	capability Capability
	kinds      []string
	missing    bool
}

func (r testResolver) Kinds() []string {
	if len(r.kinds) != 0 {
		return r.kinds
	}
	return []string{"ArtifactServer"}
}

func (r testResolver) Resolve(kind, implementation string) (Capability, bool) {
	if r.missing || !slices.Contains(r.Kinds(), kind) {
		return nil, false
	}
	return r.capability, true
}

type testCompiler struct {
	state *compilation.State
	err   error
}

func (c testCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	if c.err != nil {
		return nil, nil, c.err
	}
	return c.state, &compilation.Report{}, nil
}

type testBinder struct {
	bound    []string
	released []string
	issued   int
	material map[string]secrets.Material
	bindErr  error
}

func (b *testBinder) Bind(_ context.Context, request custody.BindRequest) (secretstore.Binding, error) {
	if b.bindErr != nil {
		return secretstore.Binding{}, b.bindErr
	}
	b.bound = append(b.bound, request.Names...)
	b.issued++
	return secretstore.Binding{ID: fmt.Sprintf("bind-%d", b.issued)}, nil
}

// Reopen models the store: a released binding is gone, so an operation that
// still needs its material can no longer acquire it.
func (b *testBinder) Reopen(_ context.Context, request custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	if slices.Contains(b.released, request.BindingID) {
		return nil, errors.New("secret binding does not exist")
	}
	out := []secretstore.BoundMaterial{}
	for name, value := range b.material {
		out = append(out, secretstore.BoundMaterial{
			Version:  secretstore.Version{Declaration: secrets.VersionDeclaration{Name: name}},
			Material: value,
		})
	}
	return out, nil
}

func (b *testBinder) Release(_ context.Context, request custody.BindingRequest) (bool, error) {
	b.released = append(b.released, request.BindingID)
	return true, nil
}

type testHost struct {
	identity controller.InstalledHostIdentity
}

func (h testHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return h.identity, nil
}

type testAutomation struct{ digest string }

func (a testAutomation) CatalogDigest() string { return a.digest }

type testGuard struct{ calls int }

func (g *testGuard) WithPython(ctx context.Context, area prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) error {
	g.calls++
	return use(prerequisites.PythonLaunch{Loader: "/loader"}, func() error { return nil })
}

type testBundle struct{}

func (testBundle) Read(context.Context, string, int) ([]byte, error) { return nil, nil }
func (testBundle) Write(context.Context, string, []byte, bool) error { return nil }
func (testBundle) EnsureDirectory(context.Context, string) error     { return nil }
func (testBundle) Entries(context.Context) ([]prerequisites.BundleEntry, error) {
	return nil, nil
}
func (testBundle) Verify(context.Context) error { return nil }
func (testBundle) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/bundle"}, nil
}

type testConfirmer struct {
	asked   int
	decline bool
}

func (c *testConfirmer) Confirm(context.Context, string, string) error {
	c.asked++
	if c.decline {
		return errors.New("declined")
	}
	return nil
}

type testPresenter struct{ presented []PlanResult }

func (p *testPresenter) PresentLifecyclePlan(_ context.Context, result PlanResult) error {
	p.presented = append(p.presented, result)
	return nil
}

type testClock struct{ moment time.Time }

func (c *testClock) Now() time.Time { c.moment = c.moment.Add(time.Second); return c.moment }

type harness struct {
	service    Service
	workspace  *testWorkspace
	capability *testCapability
	binder     *testBinder
	confirmer  *testConfirmer
	presenter  *testPresenter
	guard      *testGuard
	entropy    []byte
}

func definition(id string) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{
		ID: id, Description: "serve " + id, Stage: reconciliation.StageInfraComponents,
		Kind: "ArtifactServer", Object: id,
		Implementation: "artifact-server-nginx-v1", ContentDigest: strings.Repeat("c", 64),
		Request: json.RawMessage(`{"name":"` + id + `"}`),
		Groups:  []reconciliation.Group{{ID: "pull-image", Description: "acquire the pinned server image", Machines: []string{"controller"}}},
	}
}

func newHarness(t *testing.T, blocks ...string) *harness {
	t.Helper()
	definitions := make([]reconciliation.BlockDefinition, 0, len(blocks))
	for _, id := range blocks {
		definitions = append(definitions, definition(id))
	}
	return newPlannedHarness(t, definitions)
}

func newPlannedHarness(t *testing.T, definitions []reconciliation.BlockDefinition) *harness {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
	))})
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	workspace := &testWorkspace{
		area: newArea(), evidence: pristine,
		inputs: desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n")),
		}},
		controller: prerequisites.StorageView{
			Exists: true, Initialized: true,
			State: prerequisites.HostState{
				Host: host,
				Receipt: prerequisites.SetupReceipt{
					Status: "complete", CatalogDigest: strings.Repeat("b", 64),
					Definition: &prerequisites.Definition{},
				},
				Bindings: []prerequisites.ControllerBinding{{Context: testContextName, Machine: "controller", HostDigest: digest}},
			},
			OpenBundle: func(context.Context, string) (prerequisites.BundleArea, error) { return testBundle{}, nil },
		},
	}
	capability := &testCapability{definitions: definitions, secrets: []string{"artifact-server-tls"}}
	binder := &testBinder{material: map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(nil)}}
	confirmer := &testConfirmer{}
	presenter := &testPresenter{}
	guard := &testGuard{}
	clock := &testClock{moment: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
	h := &harness{workspace: workspace, capability: capability, binder: binder, confirmer: confirmer, presenter: presenter, guard: guard}
	h.entropy = []byte{1}
	h.service = New(workspace, nil, testCompiler{state: compilation.NewState(catalog, catalog, nil)}, binder,
		testHost{identity: host}, testAutomation{digest: testAutomaton}, guard, testResolver{capability: capability},
		Options{
			Confirmer: confirmer, Presenter: presenter, Clock: clock,
			Entropy: func(buffer []byte) (int, error) {
				for index := range buffer {
					buffer[index] = h.entropy[0]
				}
				h.entropy[0]++
				return len(buffer), nil
			},
			Selection:  func(context.Context) (string, error) { return testContextName, nil },
			Executable: Executable{Version: "devel", Commit: "abcdef1"},
			Operations: func(area operationstore.Area) OperationStore { return operationstore.New(area, clock.Now) },
		})
	return h
}

func TestFreshApplyRegistersExecutesAndProjectsEvidence(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result == nil {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if result.Receipt.State != "done" || result.Receipt.Next != "destroy" || !reconciliation.ValidOperationID(result.Receipt.Operation) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{"artifact-server-lab"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	if !slices.Equal(h.binder.bound, []string{"artifact-server-tls"}) {
		t.Fatalf("bound secrets = %v", h.binder.bound)
	}
	applied, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationDone)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := applied.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
	}
	if len(h.workspace.reservations) == 0 && len(h.capability.reservations) != 0 {
		t.Fatal("the operation did not publish its host reservations")
	}
	if h.guard.calls != 1 {
		t.Fatalf("execution guard calls = %d", h.guard.calls)
	}
	if len(result.Logs) == 0 {
		t.Fatal("the operation created no private log")
	}
}

// Setup prepares a host without claiming any context, so the first apply is
// what records this context against it. A later apply revalidates exactly that
// relationship instead of publishing another one.
func TestFirstApplyBindsTheContextToItsControllerHost(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Bindings = nil
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	bindings := h.workspace.controller.State.Bindings
	if len(bindings) != 1 || bindings[0].Context != testContextName || bindings[0].Machine != "controller" {
		t.Fatalf("bindings = %#v", bindings)
	}
	if h.workspace.binds != 1 {
		t.Fatalf("binds = %d", h.workspace.binds)
	}
	// Destroying and applying again revalidates the same binding.
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.workspace.controller.State.Bindings) != 1 {
		t.Fatalf("a repeated apply republished the binding: %#v", h.workspace.controller.State.Bindings)
	}
}

// A context already bound to another controller Machine is never silently
// rebound, and the refusal happens before the operation registers.
func TestApplyRefusesToRebindAnEstablishedContext(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Bindings = []prerequisites.ControllerBinding{
		{Context: testContextName, Machine: "replacement", HostDigest: h.workspace.controller.State.Bindings[0].HostDigest},
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if diagnostics.Of(err)[0].Code != "controller.identity" {
		t.Fatalf("rebind error = %v", err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused binding still executed %v", h.capability.applies)
	}
}

// An unprepared host refuses before any effect and names setup, because no
// context can claim a host that has none of the shared prerequisites.
func TestApplyRefusesAnUnpreparedControllerHost(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Receipt.Status = "pending"
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if diagnostics.Of(err)[0].Code != "controller.identity" || !strings.Contains(diagnostics.Of(err)[0].Remediation, "bootwright setup") {
		t.Fatalf("unprepared host error = %v", err)
	}
	if len(h.capability.applies) != 0 || h.workspace.binds != 0 {
		t.Fatalf("a refused host still executed %v", h.capability.applies)
	}
}

type testProgress struct{ rows []string }

func (p *testProgress) ReportProgress(_ context.Context, event ProgressEvent) {
	row := event.Description + ":" + event.Detail + ":" + event.Status
	if event.Total != 0 {
		row += ":" + strconv.Itoa(event.Position) + "/" + strconv.Itoa(event.Total)
	}
	p.rows = append(p.rows, row)
}

// Progress names each block by its description and each group by the frozen
// plan's own description of it, so the operator never reads an identifier.
func TestProgressNamesBlocksAndGroupsFromTheFrozenPlan(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	progress := &testProgress{}
	h.service.options.Progress = progress
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"serve artifact-server-lab::running:1/1",
		"serve artifact-server-lab:acquire the pinned server image:running:1/1",
		"serve artifact-server-lab:acquire the pinned server image:ok:1/1",
		"serve artifact-server-lab::done:1/1",
	}
	if !slices.Equal(progress.rows, want) {
		t.Fatalf("progress = %q, want %q", progress.rows, want)
	}
}

func TestDeclinedConfirmationRegistersNothing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.confirmer.decline = true
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab"}); err == nil {
		t.Fatal("a declined apply succeeded")
	}
	if h.confirmer.asked != 1 || len(h.presenter.presented) != 1 {
		t.Fatalf("confirmation asked=%d presented=%d", h.confirmer.asked, len(h.presenter.presented))
	}
	if len(h.workspace.area.files) != 0 {
		t.Fatalf("a declined apply wrote operation records: %v", slices.Collect(maps.Keys(h.workspace.area.files)))
	}
	if len(h.binder.bound) != 0 || len(h.workspace.reservations) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a declined apply bound, reserved or mutated")
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatal("a declined apply changed the context evidence")
	}
}

func TestAuthorizationTokenAndBorrowedCredentialsRefuseBeforeAnyRead(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.authorization" {
		t.Fatalf("authorization refusal = %q", code)
	}
	_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true, SSH: borrowedOptions()})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("borrowed credential refusal = %q", code)
	}
	if len(h.presenter.presented) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a refused request presented a plan or mutated")
	}
}

func TestUnsupportedObjectsRefuseBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.unsupported = []string{"ContainerCluster/sno", "Machine/guest"}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if err == nil || len(reported) != 1 || !strings.Contains(reported[0].Message, "ContainerCluster/sno") || !strings.Contains(reported[0].Remediation, supportedExample) {
		t.Fatalf("unsupported refusal = %+v", reported)
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("an unsupported graph registered an operation")
	}
}

func TestAppliedContextRefusesASecondApplyAndDestroysInstead(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("second apply = %q", code)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" || result.Receipt.Next != "none" {
		t.Fatalf("destroy = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("a completed destroy left evidence %q", h.workspace.evidence)
	}
	if len(h.workspace.reservations) != 0 {
		t.Fatal("a completed destroy retained its host reservations")
	}
	if len(h.binder.released) == 0 {
		t.Fatal("a completed destroy retained its Secret bindings")
	}
}

func TestDestroyWithoutAnAppliedOperationRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("destroy without apply = %q", code)
	}
}

func TestFailedBlockLeavesTheOperationContinuable(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil || result.Receipt.State != "failed" || result.Receipt.Next != "continue-apply" {
		t.Fatalf("failed apply = %+v (%v)", result, err)
	}
	applied, _ := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationFailed)
	want, _ := applied.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q", h.workspace.evidence)
	}
	retried, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || retried.Receipt.State != "done" {
		t.Fatalf("retry = %+v (%v)", retried, err)
	}
	if len(h.capability.applies) != 2 {
		t.Fatalf("retry ran %d attempts", len(h.capability.applies))
	}
	if h.presenter.presented[1].Continuation != true {
		t.Fatal("a continuation was presented as a fresh plan")
	}
}

func TestUnknownOutcomeIsResolvedFromLiveEvidence(t *testing.T) {
	for name, tc := range map[string]struct {
		effect reconciliation.EffectState
		state  string
		next   string
	}{
		"completed": {reconciliation.EffectCompleted, "done", "destroy"},
		"no effect": {reconciliation.EffectNoEffect, "failed", "continue-apply"},
		"unknown":   {reconciliation.EffectUnknown, "unknown", "resolve"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err == nil || result.Receipt.State != "unknown" || result.Receipt.Next != "resolve" {
				t.Fatalf("unknown apply = %+v (%v)", result, err)
			}
			h.capability.observations = []Observation{{Effect: tc.effect}}
			resolved, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if resolved.Receipt.State != tc.state || resolved.Receipt.Next != tc.next {
				t.Fatalf("resolution = %+v (%v), want %s/%s", resolved.Receipt, err, tc.state, tc.next)
			}
			if len(h.capability.observes) != 1 {
				t.Fatalf("observations = %v", h.capability.observes)
			}
			if tc.effect != reconciliation.EffectCompleted && len(h.capability.applies) != 1 {
				t.Fatal("an unresolved block started another attempt")
			}
		})
	}
}

func TestContinuationRefusesDriftedInputExecutableOrHost(t *testing.T) {
	for name, corrupt := range map[string]func(*harness){
		"changed automation": func(h *harness) {
			h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
		},
		"changed input": func(h *harness) {
			h.workspace.inputs.Files = []desiredstate.SourceFile{
				desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment # edited\n")),
			}
		},
		"unbound host": func(h *harness) {
			h.workspace.controller.State.Bindings = nil
		},
		"incomplete setup": func(h *harness) {
			h.workspace.controller.State.Receipt.Status = "pending"
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("the first attempt should have failed")
			}
			before := len(h.capability.applies)
			corrupt(h)
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err == nil {
				t.Fatal("a drifted continuation ran")
			}
			if len(h.capability.applies) != before {
				t.Fatal("a drifted continuation performed an effect")
			}
		})
	}
}

func TestMissingImplementationRefusesTheBlock(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.service.capabilities = testResolver{missing: true}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatal("a missing capability still ran")
	}
}

func TestRequiredLogFaultStopsTheOperation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.area.fail["append "+path.Join("op-"+strings.Repeat("01", 16), "logs", "operation.jsonl")] = errors.New("no space")
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("log fault = %q (%v)", code, err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatal("an effect ran after the logging boundary failed")
	}
}

func TestPlanPreviewsWithoutWritingAnything(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || result.Receipt.State != "preview" || result.Receipt.Next != "apply" || result.Receipt.Operation != "none" {
		t.Fatalf("plan = %+v (%v)", result, err)
	}
	if len(result.Steps) != 1 || result.Steps[0].ID != "artifact-server-lab" {
		t.Fatalf("steps = %+v", result.Steps)
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("plan wrote durable state")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || after.Receipt.Next != "destroy" || after.Verb != "destroy" {
		t.Fatalf("plan after apply = %+v (%v)", after, err)
	}
}

func TestStatusReportsDurableStateWithoutProbing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	before, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || before.Lifecycle != nil {
		t.Fatalf("status before apply = %+v (%v)", before, err)
	}
	if len(before.SetupChecks) != 2 || before.SetupChecks[0].Status != "ready" {
		t.Fatalf("setup checks = %+v", before.SetupChecks)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || after.Lifecycle == nil || after.Lifecycle.State != "done" || after.Lifecycle.Next != "destroy" {
		t.Fatalf("status after apply = %+v (%v)", after, err)
	}
	if len(after.Lifecycle.Blocks) != 1 || after.Lifecycle.Blocks[0].State != "done" {
		t.Fatalf("status blocks = %+v", after.Lifecycle.Blocks)
	}
}

func TestEmptyCurrentSelectionRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.service.options.Selection = func(context.Context) (string, error) { return "", nil }
	if _, err := h.service.Plan(context.Background(), PlanRequest{}); firstCode(err) != "context.state" {
		t.Fatalf("empty selection = %v", err)
	}
}

func TestCancellationLeavesTheOperationUnknown(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	ctx, cancel := context.WithCancel(context.Background())
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	cancel()
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatalf("a canceled apply reported success: %+v", result)
	}
}

func TestBlocksExecuteInFrozenOrderAndSkipDoneWork(t *testing.T) {
	h := newHarness(t, "alpha", "bravo")
	h.capability.outcomes = []Result{
		{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)},
		{Outcome: reconciliation.OutcomeFailed},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the second block should have failed")
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "bravo"}) {
		t.Fatalf("execution order = %v", h.capability.applies)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "bravo", "bravo"}) {
		t.Fatalf("continuation re-ran a completed block: %v", h.capability.applies)
	}
}

func TestOperationRecordsSurviveAnInterruptedRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.area.fail["replace index.json"] = errors.New("interrupted")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	if len(h.capability.applies) != 0 {
		t.Fatal("an effect ran before registration committed")
	}
	delete(h.workspace.area.fail, "replace index.json")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("recovery = %+v (%v)", result, err)
	}
}

func borrowedOptions() machine.SSHOptions { return machine.SSHOptions{User: "operator"} }

func firstCode(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return ""
	}
	return reported[0].Code
}

func stagedDefinition(id string, stage reconciliation.Stage, dependencies ...string) reconciliation.BlockDefinition {
	block := definition(id)
	block.Stage = stage
	slices.Sort(dependencies)
	block.Dependencies = dependencies
	return block
}

// nestedDefinitions are the shape that makes stages a graph rather than
// strata: the KubeVirt substrate of a hub cluster cannot exist before the
// hosting cluster's virtualization add-on has been installed.
func nestedDefinitions() []reconciliation.BlockDefinition {
	return []reconciliation.BlockDefinition{
		stagedDefinition("artifacts", reconciliation.StageInfraComponents),
		stagedDefinition("provider-metal", reconciliation.StageSubstrates),
		stagedDefinition("host-node", reconciliation.StageMachines, "provider-metal"),
		stagedDefinition("host-cluster", reconciliation.StageClusters, "host-node"),
		stagedDefinition("host-virtualization", reconciliation.StageAddOns, "host-cluster"),
		stagedDefinition("provider-kubevirt", reconciliation.StageSubstrates, "host-virtualization"),
		stagedDefinition("hub-node", reconciliation.StageMachines, "provider-kubevirt"),
	}
}

func TestStagedApplyPausesAtTheStageBoundary(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	})
	if err != nil {
		t.Fatalf("a stage boundary reported a failure: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationPaused) || result.Receipt.Next != "continue-apply" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{"artifacts"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	paused, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationPaused)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := paused.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
	}
}

// A stage selection gates which blocks start; it never narrows the frozen plan,
// so a block whose dependency belongs to an unselected stage simply waits.
func TestStagedApplyDefersBlocksBehindUnselectedStages(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.applies, []string{"provider-metal"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	presented := h.presenter.presented[0]
	if len(presented.Steps) != len(nestedDefinitions()) {
		t.Fatalf("the presented plan was narrowed to %d steps", len(presented.Steps))
	}
	marks := map[string]string{}
	waits := map[string]string{}
	for _, step := range presented.Steps {
		marks[step.ID], waits[step.ID] = step.Selection, step.WaitsOn
	}
	if marks["provider-metal"] != StepStart || marks["artifacts"] != StepNotSelected {
		t.Fatalf("selection markers = %v", marks)
	}
	if marks["provider-kubevirt"] != StepWaiting || waits["provider-kubevirt"] != "host-virtualization" {
		t.Fatalf("the nested substrate is not deferred behind its add-on: %v %v", marks, waits)
	}
	if presented.Startable != 1 || presented.Deferred != len(nestedDefinitions())-1 {
		t.Fatalf("startable = %d, deferred = %d", presented.Startable, presented.Deferred)
	}
}

func TestContinuationAcceptsAWiderStageSetAndCompletes(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "destroy" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{
		"artifacts", "provider-metal", "host-node", "host-cluster", "host-virtualization", "provider-kubevirt", "hub-node",
	}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
}

// A pause owns exactly what it completed, so its removal covers those blocks
// and nothing the operation never started.
func TestDestroyFromAPausedApplyRemovesOnlyDoneBlocks(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components", "substrates"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.destroys, []string{"provider-metal", "artifacts"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
}

func TestStageSelectionWithNothingStartableRefuses(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"clusters"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" {
		t.Fatalf("fresh refusal = %+v", reported)
	}
	if !strings.Contains(reported[0].Remediation, "infra-components") {
		t.Fatalf("the refusal does not name a stage that would unblock work: %+v", reported[0])
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a refused stage selection registered an operation")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"clusters"},
	})
	if code := firstCode(err); code != "lifecycle.stage" {
		t.Fatalf("continuation refusal = %q", code)
	}
}

func TestFailedBlockOutsideTheSelectionRefusesRetry(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err == nil {
		t.Fatal("a failed block reported success")
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || !strings.Contains(reported[0].Remediation, "infra-components") {
		t.Fatalf("retry refusal = %+v", reported)
	}
}

// Resolution is read-only, so an unproved effect is observed whatever stages
// the invocation selects; nothing else may start until it is resolved.
func TestUnknownBlockIsResolvedRegardlessOfStage(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err == nil {
		t.Fatal("an unknown effect reported success")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.observes, []string{"artifacts"}) {
		t.Fatalf("observed blocks = %v", h.capability.observes)
	}
	if !slices.Equal(h.capability.applies, []string{"artifacts", "provider-metal"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	if result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
}

func TestPlanPreviewMarksStartDeferredAndNotSelected(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab", Stages: []string{"substrates"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Stages, []string{"substrates"}) || result.Startable != 1 {
		t.Fatalf("preview = %+v", result)
	}
	if result.Receipt.State != "preview" || h.workspace.mutations != 0 {
		t.Fatalf("a preview allocated state: %+v", result.Receipt)
	}
	plain, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plain.Steps {
		if step.Selection != "" {
			t.Fatalf("a preview without a selection marked %s as %q", step.ID, step.Selection)
		}
		if step.Stage == "" {
			t.Fatalf("step %s carries no stage", step.ID)
		}
	}
}

// Every plan block needs a resolved capability, so an object of a kind no
// capability claims refuses the whole operation before it registers.
func TestUnclaimedKindsRefuseBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(
			api.FieldValue{Name: "management", Value: api.StringValue("managed")},
		)),
		api.NewObject(api.Proxy, "upstream", api.Value{}, api.MapValue(
			api.FieldValue{Name: "management", Value: api.StringValue("external")},
		)),
	})
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "Proxy/lab-proxy") {
		t.Fatalf("unclaimed refusal = %+v", reported)
	}
	if strings.Contains(reported[0].Message, "Proxy/upstream") {
		t.Fatal("an external service was reported as unrealizable")
	}
}

// The clients a controller block installs are what every other block's adapter
// runs, so the engine makes each of them wait for it. The edge is a real block
// dependency, frozen with the plan, not a rule about stage order.
func TestControllerBlockPrecedesEveryOtherBlock(t *testing.T) {
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	h := newPlannedHarness(t, definitions)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.applies) == 0 || h.capability.applies[0] != "controller-prerequisites" {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
}

// Selecting only a later stage therefore starts nothing at all, and the refusal
// names the stage that would unblock the operation.
func TestSelectingALaterStageWaitsForTheControllerBlock(t *testing.T) {
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	h := newPlannedHarness(t, definitions)
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	})
	if diagnostics.Of(err)[0].Code != "lifecycle.stage" || !strings.Contains(diagnostics.Of(err)[0].Remediation, "--stage controller") {
		t.Fatalf("selection error = %v", err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused selection still executed %v", h.capability.applies)
	}
}

// A plan without a controller block keeps exactly the dependencies its
// capabilities declared.
func TestPlanWithoutAControllerBlockGainsNoDependency(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != len(nestedDefinitions()) {
		t.Fatalf("steps = %+v", result.Steps)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatalf("an unrelated selection was refused: %v", err)
	}
}

// A registered operation owns its binding until a completed destroy releases
// it. Releasing it when an effect fails would strand the operation: every
// continuation reopens that exact binding.
func TestFailedExecutionKeepsItsBindingAndFailedRegistrationReleasesIt(t *testing.T) {
	failed := newHarness(t, "artifact-server-lab")
	failed.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := failed.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	if len(failed.binder.released) != 0 {
		t.Fatalf("a registered operation released its binding: %v", failed.binder.released)
	}

	refused := newHarness(t, "artifact-server-lab")
	refused.workspace.area.fail["replace index.json"] = errors.New("interrupted")
	if _, err := refused.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	if !slices.Equal(refused.binder.released, []string{"bind-1"}) {
		t.Fatalf("a failed registration retained its binding: %v", refused.binder.released)
	}
}

// The terminal state alone says only that the operation did not complete. A
// block that refused for a nameable reason must carry that reason out with it.
func TestFailedBlockReportsItsOwnCauseBesideTheTerminalState(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.applyErr = diagnostics.NewFailureWithRemediation(
		"secret.part", "the serving certificate does not cover every address its HTTPS endpoints answer on",
		"", "add 192.0.2.1 to the certificate's subject alternative names and regenerate it")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil || result.Receipt.State != "failed" {
		t.Fatalf("failed apply = %+v (%v)", result, err)
	}
	codes := map[string]string{}
	for _, reported := range diagnostics.Of(err) {
		codes[reported.Code] = reported.Remediation
	}
	if _, ok := codes["lifecycle.state"]; !ok {
		t.Fatalf("the terminal state was dropped: %v", diagnostics.Of(err))
	}
	if codes["secret.part"] != "add 192.0.2.1 to the certificate's subject alternative names and regenerate it" {
		t.Fatalf("the block cause did not reach the caller: %v", diagnostics.Of(err))
	}
}

// A controller block is the one block that extends the host's shared
// prerequisites, so the engine hands it the publication boundary that work
// needs: the retained setup evidence, the shared client area and its sealing,
// the durable identity record, the before-state publication and the native
// package lock it has to hand back before its own transaction.
func TestControllerBlockReceivesItsPublicationBoundary(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.executions) != 1 {
		t.Fatalf("executions = %d", len(h.capability.executions))
	}
	execution := h.capability.executions[0]
	if execution.Attempt != 1 || execution.Resolution != 0 {
		t.Fatalf("attempt identity = %d/%d", execution.Attempt, execution.Resolution)
	}
	if !execution.Setup.Exists || execution.Setup.State.Receipt.Status != "complete" {
		t.Fatalf("setup evidence = %+v", execution.Setup.State.Receipt)
	}
	for name, supplied := range map[string]bool{
		"ClientArea":         execution.ClientArea != nil,
		"SealClientArea":     execution.SealClientArea != nil,
		"RetainDependencies": execution.RetainDependencies != nil,
		"Prepare":            execution.Prepare != nil,
		"ReleaseFoundation":  execution.ReleaseFoundation != nil,
	} {
		if !supplied {
			t.Fatalf("the controller block received no %s capability", name)
		}
	}
	if err := execution.RetainDependencies(context.Background(), nil, nil); err != nil {
		t.Fatalf("retention refused: %v", err)
	}
	if _, err := execution.ClientArea(context.Background(), strings.Repeat("d", 64)); err != nil {
		t.Fatalf("client area refused: %v", err)
	}
	if err := execution.SealClientArea(context.Background(), strings.Repeat("d", 64)); err != nil {
		t.Fatalf("sealing refused: %v", err)
	}
}

// An observation is read-only. It may never authorize a host effect, so the
// before-state publication that precedes one is refused during a resolution.
func TestObservationCannotAuthorizeAHostEffect(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	})
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown attempt reported success")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.observes) != 1 {
		t.Fatalf("observations = %v", h.capability.observes)
	}
	observation := h.capability.executions[len(h.capability.executions)-1]
	if observation.Resolution == 0 {
		t.Fatalf("the observation carries no resolution identity: %+v", observation)
	}
	if err := observation.Prepare(context.Background(), prerequisites.NativePreparation{}); err == nil {
		t.Fatal("an observation published a before-state")
	}
}
