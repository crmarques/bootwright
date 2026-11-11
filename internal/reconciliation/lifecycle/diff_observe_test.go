package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type scriptedObservation struct {
	observation Observation
	err         error
	warnings    []operationstore.Warning
}

type projectingCapability struct {
	*testCapability
	paths     []ProjectedPath
	answers   map[string]scriptedObservation
	project   func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error)
	lock      sync.Mutex
	projected []string
	keeps     int
}

func (p *projectingCapability) Observe(ctx context.Context, execution Execution) (Observation, error) {
	if _, err := p.testCapability.Observe(ctx, execution); err != nil {
		return Observation{}, err
	}
	if scripted, found := p.answers[execution.Block.ID]; found {
		if len(scripted.warnings) != 0 && execution.Warn != nil {
			execution.Warn(ctx, scripted.warnings)
		}
		return scripted.observation, scripted.err
	}
	return Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"image":"v1"}`)}, nil
}

func (p *projectingCapability) ProjectedPaths() []ProjectedPath { return p.paths }

func (p *projectingCapability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]Projection, error) {
	p.lock.Lock()
	p.projected = append(p.projected, block.ID)
	p.lock.Unlock()
	if p.project != nil {
		return p.project(applied, block, evidence)
	}
	var observed struct{ Image string }
	if err := json.Unmarshal(evidence, &observed); err != nil {
		return nil, err
	}
	return []Projection{{Kind: api.ArtifactServer, Name: block.Object, Observed: api.MapValue(api.FieldValue{Name: "image", Value: api.StringValue(observed.Image)})}}, nil
}

func (p *projectingCapability) Keeps(reconciliation.Block) (KeptEntry, bool, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.keeps++
	return KeptEntry{}, false, nil
}

func (p *projectingCapability) Keep(context.Context, Execution) ([]Produced, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.keeps++
	return nil, nil
}

func imageObservation(image string, effect reconciliation.EffectState) scriptedObservation {
	return scriptedObservation{observation: Observation{Effect: effect, Evidence: json.RawMessage(`{"image":"` + image + `"}`)}}
}

func observedFixture(t *testing.T, names ...string) (diffFixture, *projectingCapability, string) {
	t.Helper()
	f := newDiffHarness(t, names...)
	operation := f.apply(t)
	projecting := &projectingCapability{
		testCapability: f.capability, answers: map[string]scriptedObservation{},
		paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}},
	}
	f.service.capabilities = testResolver{capability: projecting}
	return f, projecting, operation
}

func (f diffFixture) observe(t *testing.T) *DiffResult {
	t.Helper()
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab", Observe: true})
	if err != nil {
		t.Fatalf("diff --observe = %v", err)
	}
	return result
}

func refusedWith(t *testing.T, err error, code, message, remedy string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != code || !strings.HasPrefix(reported[0].Message, message) || (remedy != "" && reported[0].Remediation != remedy) {
		t.Fatalf("refusal = %v (%+v), want %s %q / %q", err, reported, code, message, remedy)
	}
}

func TestAnObservedDiffWritesNothingAndBindsNothing(t *testing.T) {
	scenarios := map[string]struct {
		prepare  func(t *testing.T, f diffFixture)
		observed int
	}{
		"a completed apply": {func(*testing.T, diffFixture) {}, 2},
		"a changed input": {func(_ *testing.T, f diffFixture) {
			f.publish("rev-55555555555555555555555555555555", "kind: Environment\nrevised: true\n", diffState(servers("v2", "alpha", "bravo")...))
		}, 2},
		"an incomplete removal": {func(t *testing.T, f diffFixture) {
			f.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
			_, _ = f.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
		}, 1},
		"a completed destroy": {func(t *testing.T, f diffFixture) { f.destroy(t) }, 0},
	}
	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			f, _, _ := observedFixture(t, "alpha", "bravo")
			scenario.prepare(t, f)
			host, binder := &countingHost{HostIdentity: f.service.host}, &countingBinder{SecretBinder: f.service.binder}
			f.service.host, f.service.binder = host, binder
			before := countWrites(f.harness)
			f.observe(t)
			want := before
			want.observes += scenario.observed
			want.guardRun += scenario.observed
			if scenario.observed != 0 {
				want.runs++
			}
			if after := countWrites(f.harness); after != want {
				t.Fatalf("an observed diff wrote, bound or ran something: want %+v, got %+v", want, after)
			}
			after := countWrites(f.harness)
			if binder.reopened > 1 || after.runEntries != 0 || after.runAreaDirectories != 0 {
				t.Fatalf("reopened %d bindings, run area %d entries and %d directories", binder.reopened, after.runEntries, after.runAreaDirectories)
			}
		})
	}
}

func TestAnObservedDiffCallsOnlyObserve(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha", "bravo", "charlie")
	applies, destroys, probes, removals, calls := len(f.capability.applies), len(f.capability.destroys), len(f.capability.probes), len(f.capability.removals), len(f.capability.calls)
	f.observe(t)
	if len(f.capability.applies) != applies || len(f.capability.destroys) != destroys || len(f.capability.probes) != probes ||
		len(f.capability.removals) != removals || projecting.keeps != 0 {
		t.Fatalf("an observed diff called more than Observe: %v", f.capability.calls)
	}
	if got := f.capability.calls[calls:]; !slices.Equal(got, []string{"observe:alpha", "observe:bravo", "observe:charlie"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestEveryInspectionIsReadOnlyAndUnrecorded(t *testing.T) {
	f, _, operation := observedFixture(t, "alpha", "bravo")
	captured := len(f.capability.executions)
	f.observe(t)
	executions := f.capability.executions[captured:]
	if len(executions) != 2 {
		t.Fatalf("captured %d inspections", len(executions))
	}
	for _, execution := range executions {
		request := RunFor(execution, Invocation{Implementation: execution.Block.Implementation, Operation: ObserveOperation})
		if !request.ReadOnly || !request.Unrecorded {
			t.Fatalf("%s runs as read-only %t and unrecorded %t", execution.Block.ID, request.ReadOnly, request.Unrecorded)
		}
		if execution.Operation != "" || execution.Attempt != 0 || execution.Resolution != 0 || execution.Log != nil || execution.Output != nil ||
			execution.Progress != nil || execution.Continuation != "" || execution.Stage != nil || execution.ApplyOperation != operation || execution.Context != "lab" {
			t.Fatalf("inspection of %s = %+v", execution.Block.ID, execution)
		}
	}
}

func TestAControllerStageInspectionCannotAuthorizeAnEffect(t *testing.T) {
	stage := definition("alpha")
	stage.Stage = reconciliation.StageController
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{stage})
	f := diffFixture{harness: h, compiler: sourcesCompiler{states: map[string]*compilation.State{}, errs: map[string]error{}}}
	f.compiler.states[digestOf(h.workspace.inputs)] = diffState(servers("v1", "alpha")...)
	h.service.compiler = f.compiler
	f.apply(t)
	captured := len(h.capability.executions)
	f.observe(t)
	inspection := h.capability.executions[captured]
	if inspection.Stage == nil || !inspection.Stage.Setup.Initialized {
		t.Fatalf("the controller stage inspection carries no read-only stage: %+v", inspection.Stage)
	}
	ctx := context.Background()
	_, clientErr := inspection.Stage.ClientArea(ctx, "client")
	for name, err := range map[string]error{
		"ClientArea": clientErr, "SealClientArea": inspection.Stage.SealClientArea(ctx, "client"),
		"RetireToolAreas":    inspection.Stage.RetireToolAreas(ctx, ""),
		"RetainDependencies": inspection.Stage.RetainDependencies(ctx, nil), "Prepare": inspection.Stage.Prepare(ctx, prerequisites.StagePreparation{}),
	} {
		t.Run(name, func(t *testing.T) {
			refusedWith(t, err, "lifecycle.state", "an observation may not authorize a host effect", "")
		})
	}
}

func TestAnUnreachableTargetRendersUnknownNeverAbsent(t *testing.T) {
	refusal := diagnostics.NewFailure("lifecycle.state", UnrecordedMaterialRefusal, "")
	for name, answer := range map[string]scriptedObservation{
		"an observation error": {err: diagnostics.NewFailure("lifecycle.adapter", "the hypervisor did not answer for the domain", "")},
		"an unknown effect":    {observation: Observation{Effect: reconciliation.EffectUnknown}},
		"an invalid effect":    {observation: Observation{Effect: "bogus"}},
		"a runner refusal":     {err: refusal},
		"a cancellation":       {err: context.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			f, projecting, _ := observedFixture(t, "alpha")
			projecting.answers["alpha"] = answer
			alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
			if alpha.Real.State != "unknown" || alpha.Real.Source != "observed" || alpha.Real.Object != nil || alpha.Real.Reason == "" || alpha.Real.Remedy == "" {
				t.Fatalf("alpha reads %+v", alpha.Real)
			}
		})
	}
}

func TestAnObservationNeedingAnOperationNamesThatInsteadOfARestore(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	projecting.answers["alpha"] = scriptedObservation{err: diagnostics.NewFailure("lifecycle.state", UnrecordedMaterialRefusal, "")}
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	if alpha.Real.State != "unknown" || alpha.Real.Reason != UnrecordedMaterialRefusal ||
		alpha.Real.Remedy != "this observation needs an operation, so bootwright diff --context lab reads this block from its records" {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
}

func TestNoEffectRendersAbsentOnlyWhenObserved(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha", "bravo")
	projecting.answers["alpha"] = scriptedObservation{observation: Observation{Effect: reconciliation.EffectNoEffect}}
	projecting.answers["bravo"] = scriptedObservation{observation: Observation{Effect: reconciliation.EffectUnknown}}
	recorded := f.diff(t)
	if alpha := objectOf(t, recorded, api.ArtifactServer, "alpha"); alpha.Real.State != "present" {
		t.Fatalf("records alone read alpha %+v", alpha.Real)
	}
	result := f.observe(t)
	alpha, bravo := objectOf(t, result, api.ArtifactServer, "alpha"), objectOf(t, result, api.ArtifactServer, "bravo")
	if alpha.Real.State != "absent" || alpha.Real.Source != "observed" || alpha.Real.Reason != "its observation proves none of it exists" {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
	if bravo.Real.State != "unknown" {
		t.Fatalf("an unknown observation read bravo %+v", bravo.Real)
	}
	if !result.Observed || result.Blocks != (DiffBlocks{Observed: 1, Unknown: 1}) {
		t.Fatalf("observed %t, blocks %+v", result.Observed, result.Blocks)
	}
}

func TestMixedBlocksFoldUnknownBeforePartialBeforePresent(t *testing.T) {
	for _, tt := range []struct {
		states []string
		want   string
	}{
		{[]string{"present", "unknown"}, "unknown"},
		{[]string{"partial", "unknown", "absent"}, "unknown"},
		{[]string{"absent", "absent"}, "absent"},
		{[]string{"present", "absent"}, "partial"},
		{[]string{"partial", "present"}, "partial"},
		{[]string{"present", "present"}, "present"},
	} {
		readings, ids := map[string]blockReading{}, []string{}
		for index, state := range tt.states {
			id := fmt.Sprintf("block-%d", index)
			readings[id], ids = blockReading{state: state}, append(ids, id)
		}
		if got := foldState(ids, readings); got != tt.want {
			t.Fatalf("fold %v = %s, want %s", tt.states, got, tt.want)
		}
	}
	server, config := definition("alpha"), definition("alpha-config")
	config.Object = "alpha"
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{server, config})
	f := diffFixture{harness: h, compiler: sourcesCompiler{states: map[string]*compilation.State{digestOf(h.workspace.inputs): diffState(servers("v1", "alpha")...)}}}
	h.service.compiler = f.compiler
	f.apply(t)
	projecting := &projectingCapability{testCapability: h.capability, paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}},
		answers: map[string]scriptedObservation{"alpha-config": {observation: Observation{Effect: reconciliation.EffectNoEffect}}}}
	h.service.capabilities = testResolver{capability: projecting}
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	if alpha.Real.State != "partial" || alpha.Real.Source != "observed" || !slices.Contains(alpha.Real.Notes, "alpha-config: its observation proves none of this effect exists") {
		t.Fatalf("a present block beside an absent one read alpha %+v", alpha.Real)
	}
}

func TestAPartialEffectStillProjects(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	projecting.answers["alpha"] = imageObservation("v2", reconciliation.EffectPartial)
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	if alpha.Real.State != "partial" || alpha.Real.Source != "observed" || alpha.Real.Object == nil || alpha.Real.Object.Spec().Get("image").Text() != "v2" {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
	if !slices.Equal(projecting.projected, []string{"alpha"}) || !slices.Contains(alpha.Real.Notes, "alpha: part of this effect is realized as its frozen request states") {
		t.Fatalf("projected %v, notes %v", projecting.projected, alpha.Real.Notes)
	}
	if alpha.Desired == nil || alpha.Desired.Spec().Get("image").Text() != "v1" || len(alpha.Real.NotCompared) != 0 {
		t.Fatalf("desired %+v, not compared %v", alpha.Desired, alpha.Real.NotCompared)
	}
}

func TestAnObjectWithoutAProjectorFallsBackToTheAppliedSide(t *testing.T) {
	f, projecting, operation := observedFixture(t, "alpha", "bravo")
	f.service.capabilities = testResolver{capability: plainObserver{projecting}}
	result := f.observe(t)
	for _, name := range []string{"alpha", "bravo"} {
		object := objectOf(t, result, api.ArtifactServer, name)
		note := "observed present; artifact-server-nginx-v1 renders no field, so the fields shown are what apply " + operation + " applied"
		if object.Real.Source != "applied" || object.Real.State != "present" || !sameObject(object.Real.Object, object.Desired) || !slices.Equal(object.Real.Notes, []string{note}) {
			t.Fatalf("%s reads %+v", name, object.Real)
		}
	}
	if result.Blocks != (DiffBlocks{Observed: 2, Unprojected: 2}) || len(projecting.projected) != 0 {
		t.Fatalf("blocks %+v, projected %v", result.Blocks, projecting.projected)
	}
}

type plainObserver struct{ observer *projectingCapability }

func (p plainObserver) Claimed(catalog api.Catalog) []string {
	return p.observer.Claimed(catalog)
}

func (p plainObserver) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	return p.observer.Plan(ctx, input)
}
func (p plainObserver) SecretReferences(block reconciliation.Block) ([]string, error) {
	return p.observer.SecretReferences(block)
}
func (p plainObserver) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	return p.observer.Removal(ctx, block)
}
func (p plainObserver) Apply(ctx context.Context, execution Execution) (Result, error) {
	return p.observer.Apply(ctx, execution)
}
func (p plainObserver) Observe(ctx context.Context, execution Execution) (Observation, error) {
	return p.observer.Observe(ctx, execution)
}
func (p plainObserver) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	return p.observer.Quiescent(ctx, probe)
}
func (p plainObserver) Destroy(ctx context.Context, execution Execution) (Result, error) {
	return p.observer.Destroy(ctx, execution)
}
func (p plainObserver) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	return p.observer.ObserveRemoval(ctx, execution)
}

func TestABlockThisBuildPlansDifferentlyIsNotObserved(t *testing.T) {
	f, _, _ := observedFixture(t, "alpha", "bravo")
	f.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
	changed := definition("bravo")
	changed.Request = json.RawMessage(`{"name":"bravo","port":8443}`)
	f.capability.definitions = []reconciliation.BlockDefinition{definition("alpha"), changed}
	observes := len(f.capability.observes)
	result := f.observe(t)
	if got := f.capability.observes[observes:]; !slices.Equal(got, []string{"alpha"}) || !slices.Equal(result.Plan.Rebuilt, []string{"bravo"}) {
		t.Fatalf("observed %v, rebuilt %v", got, result.Plan.Rebuilt)
	}
	bravo := objectOf(t, result, api.ArtifactServer, "bravo")
	if bravo.Real.State != "unknown" || bravo.Real.Reason != "this executable plans block bravo differently from the build that froze it, so its observation is not read" ||
		bravo.Real.Remedy != "install bootwright devel (abcdef1), which froze it, to observe it" {
		t.Fatalf("bravo reads %+v", bravo.Real)
	}
}

type movingWorkspace struct {
	*testWorkspace
	move func()
}

func (w movingWorkspace) RunLifecycle(ctx context.Context, name string, callback func(RunView) error) error {
	w.move()
	return w.testWorkspace.RunLifecycle(ctx, name, callback)
}

func TestAnObservedDiffRefusesWhenTheContextMovedWhileItRead(t *testing.T) {
	for name, move := range map[string]func(t *testing.T, f diffFixture){
		"its records": func(t *testing.T, f diffFixture) { f.destroy(t) },
		"its revision": func(_ *testing.T, f diffFixture) {
			f.publish("rev-88888888888888888888888888888888", "kind: Environment\nrevised: true\n", diffState(servers("v1", "alpha")...))
		},
		"its input": func(_ *testing.T, f diffFixture) {
			f.workspace.inputs = diffSources("kind: Environment\nedited: true\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := observedFixture(t, "alpha")
			f.service.workspace = movingWorkspace{testWorkspace: f.workspace, move: func() { move(t, f) }}
			observes := len(f.capability.observes)
			result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab", Observe: true})
			if result != nil || len(f.capability.observes) != observes {
				t.Fatalf("a moved context still observed: %+v", result)
			}
			refusedWith(t, err, "lifecycle.state", "the context changed while diff read it", "repeat bootwright diff --context lab --observe")
		})
	}
}

func TestAnObservedDiffRefusesAnUnreopenableBinding(t *testing.T) {
	f, _, operation := observedFixture(t, "alpha")
	f.binder.unreadable = map[string]error{"bind-1": secretstore.Failure("store.crypto", "the keyring cannot decrypt the binding")}
	runs := f.workspace.runs
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab", Observe: true})
	if result != nil || f.workspace.runs != runs {
		t.Fatalf("an unreopenable binding still observed: %+v", result)
	}
	refusedWith(t, err, "lifecycle.state",
		"the Secret binding apply "+operation+" froze cannot be reopened, so diff cannot observe its effects: the keyring cannot decrypt the binding",
		"restore the context's keyring from a complete backup, or run bootwright diff --context lab without --observe")
}

func TestAnObservedDiffRefusesAnotherHostOrAnUnboundContext(t *testing.T) {
	other, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"fedcba9876543210fedcba9876543210", "87654321-4321-8765-cba9-210fedcba987", "01234567-89ab-cdef-0123-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		prepare        func(f diffFixture)
		message, fixes string
	}{
		"another host": {func(f diffFixture) { f.service.host = testHost{identity: other} }, "this host is not the host the context is bound to", ""},
		"an unbound context": {func(f diffFixture) { f.workspace.controller.State.Bindings = nil },
			"this context is not bound to a controller host", "run bootwright diff --context lab without --observe"},
		"no setup": {func(f diffFixture) { f.workspace.controller.Prepared = false }, "this host has no completed controller setup", "run bootwright controller install"},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := observedFixture(t, "alpha")
			tt.prepare(f)
			observes := len(f.capability.observes)
			result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab", Observe: true})
			if result != nil || len(f.capability.observes) != observes {
				t.Fatalf("an unproved host still observed: %+v", result)
			}
			refusedWith(t, err, "controller.identity", tt.message, tt.fixes)
		})
	}
}

func TestProducedMaterialIsClearedAndNeverRendered(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	sentinel := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("produced-sentinel-kubeconfig")})
	projecting.answers["alpha"] = scriptedObservation{observation: Observation{
		Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"image":"v1"}`),
		Produced: []Produced{{Name: "kubeconfig", Material: sentinel}},
	}}
	result := f.observe(t)
	if value, _ := sentinel.Part(secrets.ValuePart); strings.Contains(string(value), "sentinel") {
		t.Fatalf("produced material survived the observation: %q", value)
	}
	if rendered := fmt.Sprintf("%#v", result); strings.Contains(rendered, "produced-sentinel") {
		t.Fatal("produced material reached the result")
	}
}

func TestALentSecretInAProjectionRefusesBeforeOutput(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	f.binder.material = map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("lent-private-key-bytes")})}
	projecting.answers["alpha"] = imageObservation("registry/lent-private-key-bytes", reconciliation.EffectCompleted)
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab", Observe: true})
	if result != nil {
		t.Fatalf("a lent secret reached a result: %+v", result)
	}
	refusedWith(t, err, "lifecycle.state", "a projection of block alpha carries Secret material, so diff shows nothing",
		"run bootwright diff --context lab without --observe, and report this defect")
}

func TestAnObservationsWarningsReadAsItsBlocksNotes(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha", "bravo")
	f.binder.material = map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("lent-private-key-bytes")})}
	partial := imageObservation("v1", reconciliation.EffectPartial)
	partial.warnings = []operationstore.Warning{{Code: "storage.not-ready", Message: "not ready: keepalived@ingress.rgw.objects.dc1"},
		{Code: "storage.not-ready", Message: "not ready: lent-private-key-bytes"}}
	projecting.answers["alpha"] = partial
	unknown := scriptedObservation{observation: Observation{Effect: reconciliation.EffectUnknown}, warnings: partial.warnings}
	projecting.answers["bravo"] = unknown
	result := f.observe(t)
	alpha, bravo := objectOf(t, result, api.ArtifactServer, "alpha"), objectOf(t, result, api.ArtifactServer, "bravo")
	if alpha.Real.State != "partial" || !slices.Contains(alpha.Real.Notes, "alpha: not ready: keepalived@ingress.rgw.objects.dc1") {
		t.Fatalf("a partial observation's warning is not its note: %+v", alpha.Real)
	}
	if rendered := fmt.Sprintf("%#v", result); strings.Contains(rendered, "lent-private-key-bytes") {
		t.Fatal("a warning carrying lent material reached the result")
	}
	if bravo.Real.State != "unknown" || slices.ContainsFunc(bravo.Real.Notes, func(note string) bool { return strings.Contains(note, "not ready") }) {
		t.Fatalf("an unknown reading carries its warnings: %+v", bravo.Real)
	}
}

func TestAReadyObservationsHealthWarningReadsAsItsNote(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	ready := imageObservation("v1", reconciliation.EffectCompleted)
	ready.warnings = []operationstore.Warning{{Code: "storage.health", Message: "health: POOL_APP_NOT_ENABLED names pool e2e-hand-pool"}}
	projecting.answers["alpha"] = ready
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	if alpha.Real.State != "present" || !slices.Contains(alpha.Real.Notes, "alpha: health: POOL_APP_NOT_ENABLED names pool e2e-hand-pool") {
		t.Fatalf("a ready observation's health warning is not its note: %+v", alpha.Real)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "CephCluster", Object: "storage"}}
	reported, ok := adapterWarning(reconciliation.Plan{Blocks: []reconciliation.Block{block}}, block, ready.warnings[0])
	if !ok || reported.Code != "storage.health" || reported.Severity != "warning" || reported.Object.Kind != "CephCluster" {
		t.Fatalf("a health warning reports %+v (%v)", reported, ok)
	}
}

func TestObservationsStayWithinTheConcurrencyBound(t *testing.T) {
	f, _, _ := observedFixture(t, "alpha", "bravo", "charlie", "delta", "echo")
	f.service.options.Concurrency = 2
	var lock sync.Mutex
	inFlight, widest, arrived := 0, 0, 0
	gate := make(chan struct{})
	f.capability.observeHold = func(string) {
		lock.Lock()
		inFlight++
		arrived++
		widest = max(widest, inFlight)
		if arrived == 2 {
			close(gate)
		}
		lock.Unlock()
		<-gate
		lock.Lock()
		inFlight--
		lock.Unlock()
	}
	f.observe(t)
	if widest != 2 || arrived != 5 {
		t.Fatalf("widest %d, arrived %d", widest, arrived)
	}
}

func TestCancellationStopsFurtherObservations(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha", "bravo", "charlie")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.capability.observeHold = func(string) { cancel() }
	projecting.answers["alpha"] = scriptedObservation{err: context.Canceled}
	observes := len(f.capability.observes)
	result, err := f.service.Diff(ctx, DiffRequest{ContextName: "lab", Observe: true})
	if err != nil || len(f.capability.observes) != observes+1 {
		t.Fatalf("diff = %v, observed %v", err, f.capability.observes[observes:])
	}
	for _, name := range []string{"bravo", "charlie"} {
		if object := objectOf(t, result, api.ArtifactServer, name); object.Real.State != "unknown" || object.Real.Reason != "the observation was canceled" {
			t.Fatalf("%s reads %+v", name, object.Real)
		}
	}
	if alpha := objectOf(t, result, api.ArtifactServer, "alpha"); alpha.Real.State != "unknown" || result.Blocks.Unknown != 3 {
		t.Fatalf("alpha reads %+v, blocks %+v", alpha.Real, result.Blocks)
	}
}

func TestAnInadmissibleProjectionReadsItsObjectUnknown(t *testing.T) {
	image := api.MapValue(api.FieldValue{Name: "image", Value: api.StringValue("v1")})
	for name, tt := range map[string]struct {
		projection Projection
		reason     string
	}{
		"a Secret":        {Projection{Kind: api.Secret, Name: "tls", Observed: image}, "its projection names a kind its implementation does not observe: Secret"},
		"an unknown kind": {Projection{Kind: "Bogus", Name: "alpha", Observed: image}, "its projection names a kind its implementation does not observe: Bogus"},
		"a non-mapping":   {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: api.StringValue("v1")}, "its projection of ArtifactServer/alpha is not a spec mapping"},
		"an unlisted leaf": {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: api.MapValue(api.FieldValue{Name: "retention", Value: api.StringValue("persistent")})},
			"its projection claims a field its implementation does not observe: spec.retention"},
		"an oversized note": {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: image, Notes: []string{strings.Repeat("n", 201)}},
			"its projection of ArtifactServer/alpha carries a note longer than 200 bytes or holding a control character"},
		"a control note": {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: image, Notes: []string{"deleted\x7f"}},
			"its projection of ArtifactServer/alpha carries a note longer than 200 bytes or holding a control character"},
		"a C1 control note": {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: image, Notes: []string{"next\u0085line"}},
			"its projection of ArtifactServer/alpha carries a note longer than 200 bytes or holding a control character"},
		"a path for a name": {Projection{Kind: api.ArtifactServer, Name: "../x", Observed: image},
			`its projection of ArtifactServer names an object by an invalid name: "../x"`},
		"too many notes": {Projection{Kind: api.ArtifactServer, Name: "alpha", Observed: image, Notes: make([]string, 9)},
			"its projection of ArtifactServer/alpha carries more than 8 notes"},
	} {
		t.Run(name, func(t *testing.T) {
			f, projecting, _ := observedFixture(t, "alpha")
			projecting.project = func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error) {
				return []Projection{tt.projection}, nil
			}
			alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
			if alpha.Real.State != "unknown" || alpha.Real.Reason != tt.reason {
				t.Fatalf("alpha reads %+v", alpha.Real)
			}
		})
	}
}

func TestAConflictingLeafReadsAsAppliedWithANote(t *testing.T) {
	declared := api.NewCatalog([]api.Object{api.NewObject(api.InfraProvider, "libvirt", api.Value{},
		libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80"))), profile("sno", field("diskGiB", api.IntegerValue("120")))))})
	read := diffRead{applied: diffSide{authored: declared, effective: declared}, context: ContextIdentity{Name: "lab"}}
	fragments := []fragment{
		{block: "machine-rhel-01", projection: Projection{Kind: api.InfraProvider, Name: "libvirt", Observed: libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("100"))))}},
		{block: "machine-rhel-03", projection: Projection{Kind: api.InfraProvider, Name: "libvirt", Observed: libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80"))), profile("sno", field("diskGiB", api.IntegerValue("120"))))}},
	}
	side := read.projectedSide(api.InfraProvider, "libvirt", RealSide{Source: sideObserved, State: sidePresent}, fragments)
	want := libvirtSpec(profile("rhel", field("diskGiB", api.IntegerValue("80"))), profile("sno", field("diskGiB", api.IntegerValue("120"))))
	if side.Object == nil || !side.Object.Spec().Equal(want) {
		t.Fatalf("real %+v", side.Object)
	}
	conflict := "spec.libvirt.machineProfiles[rhel].diskGiB: machine-rhel-01 observes 100, machine-rhel-03 observes 80"
	if !slices.Contains(side.Notes, conflict) || slices.Contains(side.NotCompared, "spec.libvirt.machineProfiles[rhel].diskGiB") {
		t.Fatalf("notes %v, not compared %v", side.Notes, side.NotCompared)
	}
}

func TestAnUnreadableAppliedRevisionReadsObservedUnknownWithARemedy(t *testing.T) {
	read := diffRead{appliedReason: "the applied revision is gone", context: ContextIdentity{Name: "lab"}}
	side := read.observedSide("ArtifactServer/alpha", []string{"alpha-1"}, map[string]blockReading{"alpha-1": {state: sidePresent}}, nil, claimedNames{})
	if side.State != sideUnknown || side.Reason != "the applied revision is gone" || side.Remedy != reviewStatus("lab") {
		t.Fatalf("side = %+v", side)
	}
}

func TestAProjectorThatRendersNothingCountsAsUnprojected(t *testing.T) {
	f, projecting, operation := observedFixture(t, "alpha")
	projecting.project = func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error) { return nil, nil }
	result := f.observe(t)
	alpha := objectOf(t, result, api.ArtifactServer, "alpha")
	note := "observed present; artifact-server-nginx-v1 renders no field, so the fields shown are what apply " + operation + " applied"
	if result.Blocks != (DiffBlocks{Observed: 1, Unprojected: 1}) || alpha.Real.Source != "applied" || !slices.Equal(alpha.Real.Notes, []string{note}) {
		t.Fatalf("blocks %+v, alpha %+v", result.Blocks, alpha.Real)
	}
}

func TestAProjectorErrorReadsUnknownWithAFixedReason(t *testing.T) {
	f, projecting, _ := observedFixture(t, "alpha")
	projecting.project = func(api.Catalog, reconciliation.Block, json.RawMessage) ([]Projection, error) {
		return nil, fmt.Errorf("cannot decode evidence")
	}
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	if alpha.Real.State != "unknown" || strings.Contains(alpha.Real.Reason, "decode") || alpha.Real.Reason == "" {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
}

func TestAnUnobservedProfileIsKeptAsAppliedAndNotCompared(t *testing.T) {
	for name, tt := range map[string]struct {
		bravo       scriptedObservation
		profiles    api.Value
		notCompared []string
	}{
		"bravo unknown": {scriptedObservation{observation: Observation{Effect: reconciliation.EffectUnknown}},
			libvirtSpec(profile("alpha", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("80"))), profile("bravo", field("diskGiB", api.IntegerValue("120")))),
			[]string{"spec.libvirt.machineProfiles[alpha].cpu", "spec.libvirt.machineProfiles[bravo].diskGiB"}},
		"bravo absent": {scriptedObservation{observation: Observation{Effect: reconciliation.EffectNoEffect}},
			libvirtSpec(profile("alpha", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("80"))), profile("bravo", field("diskGiB", api.IntegerValue("120")))),
			[]string{"spec.libvirt.machineProfiles[alpha].cpu", "spec.libvirt.machineProfiles[bravo].diskGiB"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiffHarness(t, "alpha", "bravo")
			provider := api.NewObject(api.InfraProvider, "libvirt", api.Value{}, libvirtSpec(
				profile("alpha", field("cpu", api.IntegerValue("4")), field("diskGiB", api.IntegerValue("80"))),
				profile("bravo", field("diskGiB", api.IntegerValue("120"))),
			))
			f.apply(t)
			f.compiler.states[digestOf(f.workspace.inputs)] = diffState(append(servers("v1", "alpha", "bravo"), provider)...)
			projecting := &projectingCapability{
				testCapability: f.capability, answers: map[string]scriptedObservation{"bravo": tt.bravo},
				paths: []ProjectedPath{{Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].name"}, {Kind: api.InfraProvider, Path: "spec.libvirt.machineProfiles[].diskGiB"}},
				project: func(_ api.Catalog, block reconciliation.Block, _ json.RawMessage) ([]Projection, error) {
					return []Projection{{Kind: api.InfraProvider, Name: "libvirt", Observed: libvirtSpec(profile(block.Object, field("diskGiB", api.IntegerValue("80"))))}}, nil
				},
			}
			f.service.capabilities = testResolver{capability: projecting}
			libvirt := objectOf(t, f.observe(t), api.InfraProvider, "libvirt")
			if libvirt.Real.State != "present" || libvirt.Desired == nil || !libvirt.Desired.Spec().Equal(provider.Spec()) || !libvirt.Real.Object.Spec().Equal(tt.profiles) {
				t.Fatalf("libvirt reads %+v against %+v", libvirt.Real, libvirt.Desired)
			}
			if !slices.Equal(libvirt.Real.NotCompared, tt.notCompared) {
				t.Fatalf("not compared = %v", libvirt.Real.NotCompared)
			}
		})
	}
}

func TestAnObservedDiffReportsOneObservationCheckRow(t *testing.T) {
	for name, tt := range map[string]struct {
		bravo  scriptedObservation
		status string
	}{
		"every block read": {imageObservation("v1", reconciliation.EffectCompleted), "ok"},
		"a block unknown":  {scriptedObservation{observation: Observation{Effect: reconciliation.EffectUnknown}}, "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			f, projecting, _ := observedFixture(t, "alpha", "bravo")
			projecting.answers["bravo"] = tt.bravo
			var events []ProgressEvent
			f.service.options.Progress = progressFunc(func(_ context.Context, event ProgressEvent) { events = append(events, event) })
			f.observe(t)
			if len(events) != 3 {
				t.Fatalf("events = %+v", events)
			}
			for index, event := range events {
				status, completed := "running", index+1
				if index == len(events)-1 {
					status, completed = tt.status, 2
				}
				if event.Phase != CheckPhase || event.Block != "observation" || event.Description != "read what this context's apply realized, changing nothing" ||
					event.Group != "" || event.Detail != "" || event.Status != status || event.Completed != completed || event.Declared != 2 {
					t.Fatalf("event %d = %+v", index, event)
				}
			}
		})
	}
}

// An observed diff reads what a removal of the owning apply would take back, so
// a completed apply's block whose record is lost is still observed.
func TestAnObservedDiffObservesACompletedApplysBlockWhoseRecordIsLost(t *testing.T) {
	f, _, operation := observedFixture(t, "alpha", "bravo", "charlie")
	lose(f.harness, path.Join(operation, "blocks", "bravo")+"/")
	calls := len(f.capability.calls)
	result := f.observe(t)
	if got := slices.Sorted(slices.Values(f.capability.calls[calls:])); !slices.Equal(got, []string{"observe:alpha", "observe:bravo", "observe:charlie"}) {
		t.Fatalf("calls = %v, want every block of the completed apply observed", got)
	}
	if side := objectOf(t, result, api.ArtifactServer, "bravo").Real; side.Source != sideObserved || side.State != sidePresent {
		t.Fatalf("bravo reads %+v, want observed present", side)
	}
}

const recordedImplementation = "artifact-image-v1"

type recordOnly struct {
	*testCapability
	proves bool
	read   []string
}

func (r *recordOnly) Plan(context.Context, PlanInput) (CapabilityPlan, error) {
	return CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}, nil
}

func (r *recordOnly) Claimed(api.Catalog) []string { return nil }

func (r *recordOnly) RecordProves(block reconciliation.Block, _ json.RawMessage) bool {
	r.read = append(r.read, block.ID)
	return r.proves
}

type recordedResolver struct {
	observed Capability
	recorded *recordOnly
}

func (r recordedResolver) Bindings() []CapabilityBinding {
	return []CapabilityBinding{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"},
		{Kind: "ArtifactServer", Implementation: recordedImplementation, Family: "artifact-image"},
	}
}

func (r recordedResolver) Resolve(kind, implementation string) (Capability, bool) {
	switch {
	case kind != "ArtifactServer":
		return nil, false
	case implementation == recordedImplementation:
		return r.recorded, true
	case implementation == "artifact-server-nginx-v1":
		return r.observed, true
	}
	return nil, false
}

func recordedFixture(t *testing.T, plain bool, prepare func(*harness)) (diffFixture, *recordOnly, string) {
	t.Helper()
	image := definition("alpha-image")
	image.Object, image.Implementation = "alpha", recordedImplementation
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), image})
	f := diffFixture{harness: h, compiler: sourcesCompiler{states: map[string]*compilation.State{digestOf(h.workspace.inputs): diffState(servers("v1", "alpha")...)}}}
	h.service.compiler = f.compiler
	projecting := &projectingCapability{testCapability: h.capability, answers: map[string]scriptedObservation{}, paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}}
	var observed Capability = projecting
	if plain {
		observed = plainObserver{projecting}
	}
	recorded := &recordOnly{testCapability: h.capability, proves: true}
	h.service.capabilities = recordedResolver{observed: observed, recorded: recorded}
	if prepare != nil {
		prepare(h)
	}
	result, _ := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if result == nil {
		t.Fatal("the apply registered nothing")
	}
	return f, recorded, result.Receipt.Operation
}

func TestANothingLiveBlockReadsItsApplysRecordInsteadOfObserving(t *testing.T) {
	f, recorded, operation := recordedFixture(t, false, nil)
	observes := len(f.capability.observes)
	result := f.observe(t)
	if got := f.capability.observes[observes:]; !slices.Equal(got, []string{"alpha"}) || !slices.Equal(recorded.read, []string{"alpha-image"}) {
		t.Fatalf("observed %v, read records of %v", got, recorded.read)
	}
	alpha := objectOf(t, result, api.ArtifactServer, "alpha")
	note := "alpha-image: artifact-image-v1 holds nothing live, so it reads apply " + operation + "'s record"
	if alpha.Real.State != "present" || alpha.Real.Source != "observed" || !slices.Contains(alpha.Real.Notes, note) {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
	if result.Blocks != (DiffBlocks{Observed: 2, Unprojected: 1}) {
		t.Fatalf("blocks %+v", result.Blocks)
	}
}

func TestANothingLiveBlockWhoseRecordProvesNothingReadsUnknown(t *testing.T) {
	for name, tt := range map[string]struct {
		prepare func(*harness)
		after   func(diffFixture, *recordOnly, string)
		reason  string
	}{
		"evidence its capability refuses": {
			after:  func(_ diffFixture, recorded *recordOnly, _ string) { recorded.proves = false },
			reason: "the record apply %s holds of block alpha-image does not prove its effect, and artifact-image-v1 holds nothing live",
		},
		"a failed attempt": {
			prepare: func(h *harness) {
				h.capability.outcomeFor = map[string]Result{"alpha-image": {Outcome: reconciliation.OutcomeFailed}}
			},
			reason: "apply %s records block alpha-image failed, and artifact-image-v1 holds nothing live",
		},
		"a lost record": {
			after: func(f diffFixture, _ *recordOnly, operation string) {
				lose(f.harness, path.Join(operation, "blocks", "alpha-image")+"/")
			},
			reason: "apply %s records block alpha-image pending, and artifact-image-v1 holds nothing live",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, recorded, operation := recordedFixture(t, false, tt.prepare)
			if tt.after != nil {
				tt.after(f, recorded, operation)
			}
			alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
			if alpha.Real.State != "unknown" || alpha.Real.Reason != fmt.Sprintf(tt.reason, operation) || alpha.Real.Remedy != reviewStatus("lab") {
				t.Fatalf("alpha reads %+v", alpha.Real)
			}
		})
	}
}

func TestAnUnrenderedObjectNamesEveryImplementationItRead(t *testing.T) {
	f, _, operation := recordedFixture(t, true, nil)
	alpha := objectOf(t, f.observe(t), api.ArtifactServer, "alpha")
	note := "observed present; artifact-image-v1, artifact-server-nginx-v1 render no field, so the fields shown are what apply " + operation + " applied"
	if alpha.Real.State != "present" || alpha.Real.Source != "applied" || !slices.Contains(alpha.Real.Notes, note) {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
}

func TestObservationsHonorTheClusterThrottle(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo", "charlie", "delta", "echo", "foxtrot")
	for i := range f.capability.definitions {
		f.capability.definitions[i].Throttle = "cluster/shared"
	}
	f.apply(t)
	f.service.capabilities = testResolver{capability: &projectingCapability{testCapability: f.capability, answers: map[string]scriptedObservation{}, paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}}}
	f.service.options.Concurrency = 8
	f.service.options.Throttle = 2
	var lock sync.Mutex
	inFlight, widest := 0, 0
	f.capability.observeHold = func(string) {
		lock.Lock()
		inFlight++
		widest = max(widest, inFlight)
		lock.Unlock()
		time.Sleep(10 * time.Millisecond)
		lock.Lock()
		inFlight--
		lock.Unlock()
	}
	f.observe(t)
	if widest > 2 {
		t.Fatalf("%d observations overlapped; throttle is 2", widest)
	}
}

func throttledDiffFixture(t *testing.T, throttled ...string) (diffFixture, *projectingCapability) {
	t.Helper()
	f := newDiffHarness(t, "alpha", "bravo", "charlie", "delta")
	for i := range f.capability.definitions {
		if slices.Contains(throttled, f.capability.definitions[i].Object) {
			f.capability.definitions[i].Throttle = "cluster/shared"
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, f.capability.definitions)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, block := range plan.Blocks {
		order = append(order, block.ID)
	}
	if !slices.Equal(order, []string{"alpha", "bravo", "charlie", "delta"}) {
		t.Fatalf("frozen order %v", order)
	}
	f.apply(t)
	projecting := &projectingCapability{testCapability: f.capability, answers: map[string]scriptedObservation{}, paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}}
	f.service.capabilities = testResolver{capability: projecting}
	f.service.options.Concurrency = 8
	f.service.options.Throttle = 1
	return f, projecting
}

func TestAFullThrottleNeverHoldsBackALaterObservation(t *testing.T) {
	f, _ := throttledDiffFixture(t, "alpha", "bravo")
	var lock sync.Mutex
	inFlight, together := map[string]bool{}, false
	gathered := make(chan struct{})
	f.capability.observeHold = func(block string) {
		lock.Lock()
		inFlight[block] = true
		if inFlight["alpha"] && inFlight["charlie"] && inFlight["delta"] && !together {
			together = true
			close(gathered)
		}
		lock.Unlock()
		if block != "bravo" {
			select {
			case <-gathered:
			case <-time.After(5 * time.Second):
			}
		}
		lock.Lock()
		delete(inFlight, block)
		lock.Unlock()
	}
	result := f.observe(t)
	if !together {
		t.Fatal("bravo, waiting on its full throttle, held back charlie and delta")
	}
	if result.Blocks.Observed != 4 || result.Blocks.Unknown != 0 || len(f.capability.observes) < 4 {
		t.Fatalf("blocks %+v, observes %v", result.Blocks, f.capability.observes)
	}
}

func TestCancellationReachesABlockWaitingOnAFullThrottle(t *testing.T) {
	f, projecting := throttledDiffFixture(t, "alpha", "bravo")
	projecting.answers["alpha"] = scriptedObservation{err: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var lock sync.Mutex
	arrived := map[string]bool{}
	others := make(chan struct{})
	var once sync.Once
	f.capability.observeHold = func(block string) {
		lock.Lock()
		arrived[block] = true
		if arrived["charlie"] && arrived["delta"] {
			once.Do(func() { close(others) })
		}
		lock.Unlock()
		select {
		case <-others:
		case <-time.After(5 * time.Second):
		}
		if block == "alpha" {
			cancel()
			return
		}
		<-ctx.Done()
	}
	observes := len(f.capability.observes)
	result, err := f.service.Diff(ctx, DiffRequest{ContextName: "lab", Observe: true})
	if err != nil {
		t.Fatalf("diff = %v", err)
	}
	if started := f.capability.observes[observes:]; slices.Contains(started, "bravo") || len(started) != 3 {
		t.Fatalf("observations started %v", started)
	}
	if bravo := objectOf(t, result, api.ArtifactServer, "bravo"); bravo.Real.State != "unknown" || bravo.Real.Reason != "the observation was canceled" {
		t.Fatalf("bravo reads %+v", bravo.Real)
	}
	if alpha := objectOf(t, result, api.ArtifactServer, "alpha"); alpha.Real.State != "unknown" {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
	if result.Blocks.Unknown != 2 || result.Blocks.Observed != 2 {
		t.Fatalf("blocks %+v", result.Blocks)
	}
}

func TestEachDiffHandsItsObservationsOneSharedReadingsOfItsOwn(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo", "charlie")
	f.apply(t)
	for _, execution := range f.capability.executions {
		if execution.Shared != nil {
			t.Fatalf("apply's block %s carries shared readings", execution.Block.ID)
		}
	}
	var diffs []*SharedReadings
	for range 2 {
		before := len(f.capability.executions)
		f.observe(t)
		observed := f.capability.executions[before:]
		if len(observed) != 3 {
			t.Fatalf("a diff observed %d blocks", len(observed))
		}
		for _, execution := range observed {
			if execution.Shared == nil || execution.Shared != observed[0].Shared {
				t.Fatalf("block %s observed with shared readings %p, its diff's first %p", execution.Block.ID, execution.Shared, observed[0].Shared)
			}
		}
		diffs = append(diffs, observed[0].Shared)
	}
	if diffs[0] == diffs[1] {
		t.Fatal("two diffs shared one reading store")
	}
}

type sharingCapability struct {
	*projectingCapability
	reads atomic.Int32
}

func (c *sharingCapability) Observe(ctx context.Context, execution Execution) (Observation, error) {
	if _, err := execution.Shared.Read(ctx, "ceph-host-reading\x00cluster", func(context.Context) ([]byte, error) {
		c.reads.Add(1)
		time.Sleep(5 * time.Millisecond)
		return []byte("hosts"), nil
	}); err != nil {
		return Observation{}, err
	}
	return c.projectingCapability.Observe(ctx, execution)
}

func TestOneThrottleSlotAndOneObservationAtATimeStillShareOneReading(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo", "charlie", "delta")
	for i := range f.capability.definitions {
		f.capability.definitions[i].Throttle = "ceph/cluster"
	}
	f.apply(t)
	sharing := &sharingCapability{projectingCapability: &projectingCapability{testCapability: f.capability, answers: map[string]scriptedObservation{}, paths: []ProjectedPath{{Kind: api.ArtifactServer, Path: "spec.image"}}}}
	f.service.capabilities = testResolver{capability: sharing}
	f.service.options.Concurrency = 1
	f.service.options.Throttle = 1
	done := make(chan *DiffResult, 1)
	go func() { done <- f.observe(t) }()
	select {
	case result := <-done:
		if result.Blocks.Observed != 4 || result.Blocks.Unknown != 0 {
			t.Fatalf("blocks %+v", result.Blocks)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("observations sharing one reading under one throttle slot deadlocked")
	}
	if sharing.reads.Load() != 1 {
		t.Fatalf("the shared reading ran %d times", sharing.reads.Load())
	}
}
