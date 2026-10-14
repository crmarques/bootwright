package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type sourcesCompiler struct {
	states map[string]*compilation.State
	errs   map[string]error
}

func (c sourcesCompiler) Compile(_ context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	digest := digestOf(sources)
	if err := c.errs[digest]; err != nil {
		return nil, nil, err
	}
	state, found := c.states[digest]
	if !found {
		return nil, nil, diagnostics.NewFailure("input.state", "these sources compile to nothing this test declared", "")
	}
	return state, &compilation.Report{}, nil
}

type diffFixture struct {
	*harness
	compiler sourcesCompiler
}

func diffSources(text string) desiredstate.Sources {
	return desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(text)),
	}}
}

func serverObject(name, image string) api.Object {
	return api.NewObject(api.ArtifactServer, name, api.Value{}, api.MapValue(api.FieldValue{Name: "image", Value: api.StringValue(image)}))
}

func diffState(objects ...api.Object) *compilation.State {
	environment := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
	))
	catalog := api.NewCatalog(append([]api.Object{environment}, objects...))
	return compilation.NewState(catalog, catalog, nil)
}

func servers(image string, names ...string) []api.Object {
	objects := make([]api.Object, 0, len(names))
	for _, name := range names {
		objects = append(objects, serverObject(name, image))
	}
	return objects
}

func newDiffHarness(t *testing.T, names ...string) diffFixture {
	t.Helper()
	h := newHarness(t, names...)
	compiler := sourcesCompiler{states: map[string]*compilation.State{}, errs: map[string]error{}}
	compiler.states[digestOf(h.workspace.inputs)] = diffState(servers("v1", names...)...)
	h.service.compiler = compiler
	return diffFixture{harness: h, compiler: compiler}
}

func (f diffFixture) publish(revision, text string, state *compilation.State) {
	sources := diffSources(text)
	f.compiler.states[digestOf(sources)] = state
	f.update(revision, sources)
}

func (f diffFixture) apply(t *testing.T) string {
	t.Helper()
	result, err := f.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("apply = %v", err)
	}
	return result.Receipt.Operation
}

func (f diffFixture) destroy(t *testing.T) string {
	t.Helper()
	result, err := f.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("destroy = %v", err)
	}
	return result.Receipt.Operation
}

func (f diffFixture) diff(t *testing.T) *DiffResult {
	t.Helper()
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"})
	if err != nil {
		t.Fatalf("diff = %v", err)
	}
	return result
}

func currentID(t *testing.T, f diffFixture) string {
	t.Helper()
	index, err := operationstore.New(f.workspace.area, func() time.Time { return time.Time{} }).Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return index.Current
}

func objectOf(t *testing.T, result *DiffResult, kind api.Kind, name string) ObjectDiff {
	t.Helper()
	for _, object := range result.Objects {
		if object.Kind == kind && object.Name == name {
			return object
		}
	}
	t.Fatalf("the diff names no %s/%s: %+v", kind, name, result.Objects)
	return ObjectDiff{}
}

func sameObject(first, second *api.Object) bool {
	return first != nil && second != nil && first.Identity() == second.Identity() && first.Value().Equal(second.Value())
}

func TestDiffWithNoOperationReadsEveryObjectAbsent(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	result := f.diff(t)
	if result.Operation.ID != "" || result.Operation.Applied != "" || result.Input != "none" || result.Plan.State != "none" {
		t.Fatalf("header = %+v, input %s, plan %+v", result.Operation, result.Input, result.Plan)
	}
	if len(result.Objects) != 2 {
		t.Fatalf("objects = %+v", result.Objects)
	}
	for _, object := range result.Objects {
		if object.Real.State != "absent" || object.Real.Object != nil || object.Real.Reason != "no operation of this context realized it" || object.Desired == nil {
			t.Fatalf("%s/%s reads %+v", object.Kind, object.Name, object.Real)
		}
	}
	if alpha := objectOf(t, result, api.ArtifactServer, "alpha"); alpha.Plan != "added" || !slices.Equal(alpha.Blocks, []string{"alpha"}) || alpha.Realization != "" {
		t.Fatalf("alpha = %+v", alpha)
	}
}

func TestDiffOfACompletedApplyOverUnchangedInputShowsNoDifference(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	operation := f.apply(t)
	result := f.diff(t)
	if result.Input != "unchanged" || result.Plan.State != "same" || result.Plan.Changed+result.Plan.Added+result.Plan.Removed != 0 {
		t.Fatalf("input %s, plan %+v", result.Input, result.Plan)
	}
	if result.Operation != (DiffOperation{ID: operation, Verb: "apply", State: "done", Apply: operation, Revision: testRevision, Applied: "readable"}) {
		t.Fatalf("operation = %+v", result.Operation)
	}
	for _, object := range result.Objects {
		if object.Real.State != "present" || object.Real.Source != "applied" || !sameObject(object.Real.Object, object.Desired) {
			t.Fatalf("%s/%s reads %+v beside %+v", object.Kind, object.Name, object.Real, object.Desired)
		}
		if object.Kind == api.ArtifactServer && (object.Realization != RealizationDone || object.Plan != "same") {
			t.Fatalf("%s = %+v", object.Name, object)
		}
	}
}

func TestDiffNamesAddedChangedAndRemovedObjects(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo", "charlie")
	operation := f.apply(t)
	changed := definition("bravo")
	changed.Request = json.RawMessage(`{"image":"v2","name":"bravo"}`)
	f.capability.definitions = []reconciliation.BlockDefinition{definition("alpha"), changed, definition("delta")}
	f.publish("rev-11111111111111111111111111111111", "kind: Environment\nrevised: true\n",
		diffState(serverObject("alpha", "v1"), serverObject("bravo", "v2"), serverObject("delta", "v1")))
	result := f.diff(t)
	if result.Input != "changed" || result.Plan.State != "differs" || result.Plan.Changed != 1 || result.Plan.Added != 1 || result.Plan.Removed != 1 {
		t.Fatalf("input %s, plan %+v", result.Input, result.Plan)
	}
	alpha, bravo := objectOf(t, result, api.ArtifactServer, "alpha"), objectOf(t, result, api.ArtifactServer, "bravo")
	charlie, delta := objectOf(t, result, api.ArtifactServer, "charlie"), objectOf(t, result, api.ArtifactServer, "delta")
	if alpha.Plan != "same" || alpha.Realization != RealizationDone || !sameObject(alpha.Real.Object, alpha.Desired) {
		t.Fatalf("alpha = %+v", alpha)
	}
	if bravo.Plan != "changed" || bravo.Realization != RealizationDone || bravo.Real.State != "present" || bravo.Desired == nil || sameObject(bravo.Real.Object, bravo.Desired) ||
		bravo.Real.Object.Spec().Get("image").Text() != "v1" {
		t.Fatalf("bravo = %+v", bravo)
	}
	if charlie.Plan != "removed" || charlie.Realization != RealizationDone || charlie.Desired != nil || charlie.Real.State != "present" || charlie.Real.Object == nil {
		t.Fatalf("charlie = %+v", charlie)
	}
	if delta.Plan != "added" || delta.Realization != "" || delta.Real.State != "absent" || delta.Real.Reason != "apply "+operation+" did not declare it" {
		t.Fatalf("delta = %+v", delta)
	}
}

func TestDiffOfABytesOnlyEditShowsNoObjectDifference(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	f.apply(t)
	f.publish("rev-22222222222222222222222222222222", "kind: Environment\n# a comment\n", diffState(servers("v1", "alpha")...))
	result := f.diff(t)
	if result.Input != "changed" || result.Plan.State != "same" {
		t.Fatalf("input %s, plan %+v", result.Input, result.Plan)
	}
	for _, object := range result.Objects {
		if object.Real.State != "present" || !sameObject(object.Real.Object, object.Desired) {
			t.Fatalf("%s/%s reads %+v beside %+v", object.Kind, object.Name, object.Real, object.Desired)
		}
	}
}

func TestDiffOfAFailedApplyReadsItsFailedObjectUnknown(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	f.capability.errorFor = map[string]error{"bravo": errors.New("the server did not start")}
	if _, err := f.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose block failed reported success")
	}
	result := f.diff(t)
	bravo := objectOf(t, result, api.ArtifactServer, "bravo")
	if bravo.Realization != RealizationFailed || bravo.Real.State != "unknown" || bravo.Real.Object != nil ||
		bravo.Real.Reason != "its blocks read failed: bravo" || bravo.Real.Remedy != reviewStatus("lab") {
		t.Fatalf("bravo = %+v", bravo)
	}
	if alpha := objectOf(t, result, api.ArtifactServer, "alpha"); alpha.Real.State != "present" || !sameObject(alpha.Real.Object, alpha.Desired) {
		t.Fatalf("alpha = %+v", alpha)
	}
}

func TestDiffOfAnIncompleteRemovalReadsTakenBackObjectsAbsent(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	applied := f.apply(t)
	f.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	_, _ = f.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	removal := currentID(t, f)
	if removal == applied || len(f.capability.destroys) != 2 {
		t.Fatalf("the removal did not run both blocks: %v", f.capability.destroys)
	}
	result := f.diff(t)
	if result.Operation.Verb != "destroy" || result.Operation.Apply != applied || result.Input != "unchanged" {
		t.Fatalf("operation %+v, input %s", result.Operation, result.Input)
	}
	taken, failed := objectOf(t, result, api.ArtifactServer, f.capability.destroys[0]), objectOf(t, result, api.ArtifactServer, f.capability.destroys[1])
	if taken.Real.State != "absent" || taken.Real.Reason != "destroy "+removal+" took it back" || taken.Desired == nil {
		t.Fatalf("taken back = %+v", taken)
	}
	if failed.Real.State != "unknown" || failed.Realization != RealizationFailed {
		t.Fatalf("failed removal = %+v", failed)
	}
	if environment := objectOf(t, result, api.Environment, "lab"); environment.Real.State != "present" {
		t.Fatalf("environment = %+v", environment)
	}
}

func TestDiffOfACompletedDestroyReadsEverythingAbsent(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	f.apply(t)
	removal := f.destroy(t)
	result := f.diff(t)
	if result.Input != "none" || len(result.Objects) != 3 {
		t.Fatalf("input %s, objects %+v", result.Input, result.Objects)
	}
	for _, object := range result.Objects {
		if object.Real.State != "absent" || object.Real.Object != nil || object.Real.Reason != "destroy "+removal+" took it back" {
			t.Fatalf("%s/%s reads %+v", object.Kind, object.Name, object.Real)
		}
	}
}

func TestDiffOfAnUnreadableFrozenRevisionReadsAppliedObjectsUnknown(t *testing.T) {
	for name, lose := range map[string]func(f diffFixture){
		"a missing revision": func(f diffFixture) { delete(f.workspace.revisions, testRevision) },
		"a digest mismatch": func(f diffFixture) {
			f.workspace.revisions[testRevision] = diffSources("kind: Environment\ntampered: true\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiffHarness(t, "alpha")
			operation := f.apply(t)
			f.publish("rev-33333333333333333333333333333333", "kind: Environment\nrevised: true\n", diffState(servers("v2", "alpha")...))
			lose(f)
			result := f.diff(t)
			prefix := "the input revision " + testRevision + " that apply " + operation + " froze cannot be read: "
			if !strings.HasPrefix(result.Operation.Applied, prefix) {
				t.Fatalf("applied = %q", result.Operation.Applied)
			}
			for _, object := range result.Objects {
				if object.Real.State != "unknown" || object.Real.Object != nil || object.Real.Reason != result.Operation.Applied {
					t.Fatalf("%s/%s reads %+v", object.Kind, object.Name, object.Real)
				}
			}
		})
	}
}

func TestDiffUnderAnotherBuildListsTheBlocksItPlansDifferently(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	f.apply(t)
	if result := f.diff(t); len(result.Plan.Rebuilt) != 0 {
		t.Fatalf("the build that froze the plan rebuilt %v", result.Plan.Rebuilt)
	}
	f.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
	if result := f.diff(t); len(result.Plan.Rebuilt) != 0 {
		t.Fatalf("another build that plans the same blocks rebuilt %v", result.Plan.Rebuilt)
	}
	changed := definition("bravo")
	changed.Request = json.RawMessage(`{"name":"bravo","port":8443}`)
	f.capability.definitions = []reconciliation.BlockDefinition{definition("alpha"), changed}
	if result := f.diff(t); !slices.Equal(result.Plan.Rebuilt, []string{"bravo"}) {
		t.Fatalf("rebuilt = %v", result.Plan.Rebuilt)
	}
}

func TestDiffOfAnUncompilableSelectedRevisionRefuses(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	f.apply(t)
	sources := diffSources("kind: Environment\nbroken: [\n")
	f.compiler.errs[digestOf(sources)] = diagnostics.NewFailureWithRemediation("api.value", "the value cannot be read", "", "repair it")
	f.update("rev-44444444444444444444444444444444", sources)
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"})
	if reported := diagnostics.Of(err); result != nil || len(reported) != 1 || reported[0].Code != "api.value" {
		t.Fatalf("diff = %+v (%v)", result, err)
	}
}

type leasedWorkspace struct{ Workspace }

func (leasedWorkspace) ReadLifecycle(context.Context, string, func(View) error) error {
	return failure("lifecycle.lease", "another operation holds this context", "wait for it to finish")
}

func TestDiffRefusesWhileAnOperationHoldsTheContext(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	f.service.workspace = leasedWorkspace{Workspace: f.workspace}
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"})
	if reported := diagnostics.Of(err); result != nil || len(reported) != 1 || reported[0].Code != "lifecycle.lease" {
		t.Fatalf("diff = %+v (%v)", result, err)
	}
}

func retireIndex(f diffFixture) {
	source := "op-" + strings.Repeat("ab", 16)
	f.workspace.area.files["index.json"] = []byte(`{"version":2,"current":"` + source + `","qualification":{"id":"qual-` + strings.Repeat("cd", 16) + `","source":"` + source + `"}}` + "\n")
}

func TestDiffRefusesARetiredIndex(t *testing.T) {
	f := newDiffHarness(t, "alpha")
	retireIndex(f)
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"})
	if reported := diagnostics.Of(err); result != nil || !errors.Is(err, operationstore.ErrRetiredIndex) || len(reported) != 1 ||
		reported[0].Code != "lifecycle.state" || reported[0].Remediation != operationstore.RetiredIndexRemedy("lab") {
		t.Fatalf("diff = %+v (%v)", result, err)
	}
}

type countingHost struct {
	HostIdentity
	asked int
}

func (h *countingHost) Identity(ctx context.Context) (controller.InstalledHostIdentity, error) {
	h.asked++
	return h.HostIdentity.Identity(ctx)
}

type countingBinder struct {
	SecretBinder
	reopened int
}

func (b *countingBinder) Reopen(ctx context.Context, request custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	b.reopened++
	return b.SecretBinder.Reopen(ctx, request)
}

func TestDiffWritesNothing(t *testing.T) {
	scenarios := map[string]func(t *testing.T, f diffFixture){
		"no operation":      func(*testing.T, diffFixture) {},
		"a completed apply": func(t *testing.T, f diffFixture) { f.apply(t) },
		"a changed input": func(t *testing.T, f diffFixture) {
			f.apply(t)
			f.publish("rev-55555555555555555555555555555555", "kind: Environment\nrevised: true\n", diffState(servers("v2", "alpha", "bravo")...))
		},
		"a failed apply": func(t *testing.T, f diffFixture) {
			f.capability.errorFor = map[string]error{"bravo": errors.New("failed")}
			_, _ = f.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		},
		"an incomplete removal": func(t *testing.T, f diffFixture) {
			f.apply(t)
			f.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
			_, _ = f.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
		},
		"a completed destroy": func(t *testing.T, f diffFixture) { f.apply(t); f.destroy(t) },
		"an unreadable frozen revision": func(t *testing.T, f diffFixture) {
			f.apply(t)
			f.publish("rev-66666666666666666666666666666666", "kind: Environment\nrevised: true\n", diffState(servers("v1", "alpha", "bravo")...))
			delete(f.workspace.revisions, testRevision)
		},
		"another build": func(t *testing.T, f diffFixture) {
			f.apply(t)
			f.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
		},
		"an uncompilable selected revision": func(t *testing.T, f diffFixture) {
			sources := diffSources("kind: Environment\nbroken: [\n")
			f.compiler.errs[digestOf(sources)] = diagnostics.NewFailure("api.value", "the value cannot be read", "")
			f.update("rev-77777777777777777777777777777777", sources)
		},
		"a retired index": func(_ *testing.T, f diffFixture) { retireIndex(f) },
	}
	for name, prepare := range scenarios {
		t.Run(name, func(t *testing.T) {
			f := newDiffHarness(t, "alpha", "bravo")
			prepare(t, f)
			host, binder := &countingHost{HostIdentity: f.service.host}, &countingBinder{SecretBinder: f.service.binder}
			f.service.host, f.service.binder = host, binder
			before := countWrites(f.harness)
			_, _ = f.service.Diff(context.Background(), DiffRequest{ContextName: "lab"})
			assertNothingWritten(t, f.harness, before)
			if host.asked != 0 || binder.reopened != 0 {
				t.Fatalf("diff asked the host %d times and reopened %d bindings", host.asked, binder.reopened)
			}
		})
	}
}

func TestDiffOrdersObjectsByKindThenName(t *testing.T) {
	f := newDiffHarness(t, "zulu", "alpha")
	tls := api.NewObject(api.Secret, "tls", api.Value{}, api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("tls")}))
	f.compiler.states[digestOf(f.workspace.inputs)] = diffState(serverObject("zulu", "v1"), tls, serverObject("alpha", "v1"))
	var order []string
	for _, object := range f.diff(t).Objects {
		order = append(order, string(object.Kind)+"/"+object.Name)
	}
	if want := []string{"Environment/lab", "ArtifactServer/alpha", "ArtifactServer/zulu", "Secret/tls"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestCanonicalOrdersNamedListsAndDropsAbsentFields(t *testing.T) {
	site := func(name, description string) api.Value {
		return api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(name)}, api.FieldValue{Name: "description", Value: api.StringValue(description)})
	}
	authored := api.MapValue(
		api.FieldValue{Name: "sites", Value: api.ListValue(site("west", "second"), site("east", "first"), site("north", "third"))},
		api.FieldValue{Name: "resources", Value: api.StringList("zeta", "alpha")},
		api.FieldValue{Name: "downloads", Value: api.Value{}},
	)
	shape := api.Schema(api.Environment)
	got := canonical(authored, shape)
	want := api.MapValue(
		api.FieldValue{Name: "sites", Value: api.ListValue(site("east", "first"), site("north", "third"), site("west", "second"))},
		api.FieldValue{Name: "resources", Value: api.StringList("zeta", "alpha")},
	)
	if !got.Equal(want) || got.Has("downloads") {
		t.Fatalf("canonical = %+v", got)
	}
	if !canonical(got, shape).Equal(got) {
		t.Fatal("canonical is not idempotent")
	}
}

func TestADoneObjectTheAppliedRevisionDoesNotDeclareReadsUnknown(t *testing.T) {
	f := newDiffHarness(t, "alpha", "bravo")
	operation := f.apply(t)
	f.publish("rev-33333333333333333333333333333333", "kind: Environment\nrevised: true\n", diffState(servers("v1", "alpha", "bravo")...))
	f.compiler.states[digestOf(f.workspace.revisions[testRevision])] = diffState(servers("v1", "alpha")...)
	result := f.diff(t)
	if result.Operation.Applied != "readable" {
		t.Fatalf("applied = %q", result.Operation.Applied)
	}
	bravo := objectOf(t, result, api.ArtifactServer, "bravo")
	if bravo.Realization != RealizationDone {
		t.Fatalf("bravo realization = %q, want done", bravo.Realization)
	}
	want := RealSide{
		Source: "applied", State: "unknown",
		Reason: "the input revision " + testRevision + " that apply " + operation + " froze does not declare it",
		Remedy: reviewStatus("lab"),
	}
	if bravo.Real.State != want.State || bravo.Real.Source != want.Source || bravo.Real.Reason != want.Reason || bravo.Real.Remedy != want.Remedy || bravo.Real.Object != nil {
		t.Fatalf("bravo reads %+v, want %+v", bravo.Real, want)
	}
	if alpha := objectOf(t, result, api.ArtifactServer, "alpha"); alpha.Real.State != "present" || alpha.Real.Object == nil {
		t.Fatalf("alpha reads %+v", alpha.Real)
	}
}
