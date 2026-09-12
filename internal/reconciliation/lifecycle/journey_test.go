package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path"
	"slices"
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
	testContextID = "ctx-0123456789abcdef0123456789abcdef"
	testRevision  = "rev-0123456789abcdef0123456789abcdef"
	testAutomaton = "aaaa0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
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
	return ContextIdentity{Name: "lab", ID: testContextID, Revision: testRevision}
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

func (v *testView) Reserve(_ context.Context, reservations []prerequisites.HostReservation) error {
	v.workspace.reservations = slices.Clone(reservations)
	return nil
}

func (v *testView) ReleaseReservations(context.Context) error {
	v.workspace.reservations = nil
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
	material     []map[string]secrets.Material
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

func (c *testCapability) Apply(_ context.Context, execution Execution) (Result, error) {
	c.applies = append(c.applies, execution.Block.ID)
	c.material = append(c.material, execution.Material)
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
	if len(c.observations) == 0 {
		return Observation{Effect: reconciliation.EffectUnknown}, nil
	}
	value := c.observations[0]
	c.observations = c.observations[1:]
	return value, nil
}

type testResolver struct {
	capability Capability
	missing    bool
}

func (r testResolver) Resolve(kind, implementation string) (Capability, bool) {
	if r.missing || kind != "ArtifactServer" {
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
	material map[string]secrets.Material
	bindErr  error
}

func (b *testBinder) Bind(_ context.Context, request custody.BindRequest) (secretstore.Binding, error) {
	if b.bindErr != nil {
		return secretstore.Binding{}, b.bindErr
	}
	b.bound = append(b.bound, request.Names...)
	return secretstore.Binding{ID: "bind-1"}, nil
}

func (b *testBinder) Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
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
		ID: id, Description: "serve " + id, Kind: "ArtifactServer", Object: id,
		Implementation: "artifact-server-nginx-v1", ContentDigest: strings.Repeat("c", 64),
		Request: json.RawMessage(`{"name":"` + id + `"}`),
	}
}

func newHarness(t *testing.T, blocks ...string) *harness {
	t.Helper()
	definitions := make([]reconciliation.BlockDefinition, 0, len(blocks))
	for _, id := range blocks {
		definitions = append(definitions, definition(id))
	}
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
		api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("bastion")})},
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
				Bindings: []prerequisites.ControllerBinding{{ContextID: testContextID, Machine: "bastion", HostDigest: digest}},
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
			Selection:  func(context.Context) (string, string, error) { return "lab", testContextID, nil },
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
	if err == nil || len(reported) != 1 || !strings.Contains(reported[0].Message, "ContainerCluster/sno") || !strings.Contains(reported[0].Remediation, "lab-artifacts") {
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

func TestStaleCurrentSelectionRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.service.options.Selection = func(context.Context) (string, string, error) {
		return "lab", "ctx-ffffffffffffffffffffffffffffffff", nil
	}
	if _, err := h.service.Plan(context.Background(), PlanRequest{}); firstCode(err) != "context.state" {
		t.Fatalf("stale selection = %v", err)
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
