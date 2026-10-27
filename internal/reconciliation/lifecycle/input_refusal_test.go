package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	changedMessage = "the desired state changed after this apply completed"
	changedRemedy  = "take back what it owns with bootwright destroy --context lab, then run bootwright apply --context lab"
	namedRemedy    = changedRemedy + "; bootwright diff --context lab shows each change"
)

func pool(name, size string) api.Object {
	return api.NewObject(api.CephPool, name, api.Value{}, api.MapValue(
		api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")},
		api.FieldValue{Name: "replicated", Value: api.MapValue(api.FieldValue{Name: "size", Value: api.IntegerValue(size)})},
	))
}

func storageClient(name string) api.Object {
	return api.NewObject(api.CephClient, name, api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}))
}

// appliedOver completes an apply of the harness's one block, then has its
// frozen input compile to before, which no capability needs to claim.
func appliedOver(t *testing.T, before *compilation.State) diffFixture {
	t.Helper()
	f := newDiffHarness(t, "alpha")
	f.apply(t)
	f.compiler.states[digestOf(f.workspace.inputs)] = before
	return f
}

// applyRefusedWith applies over what the fixture holds and requires one
// lifecycle.state refusal with exactly message and remedy.
func applyRefusedWith(t *testing.T, f diffFixture, message, remedy string) {
	t.Helper()
	_, err := f.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != remedy {
		t.Fatalf("the apply reported %+v (%v), want %q with remedy %q", reported, err, message, remedy)
	}
}

func TestAChangedInputOverACompletedApplyNamesWhatChanged(t *testing.T) {
	f := appliedOver(t, diffState(pool("a", "3"), storageClient("c")))
	f.publish("rev-2", "kind: Environment\n# edited\n", diffState(pool("a", "2"), pool("b", "3")))
	applyRefusedWith(t, f, changedMessage+": it adds CephPool/b, changes CephPool/a (spec.replicated.size) and removes CephClient/c", namedRemedy)
}

func TestANamedChangeIsBoundedToFiveObjects(t *testing.T) {
	f := appliedOver(t, diffState())
	f.publish("rev-2", "kind: Environment\n# edited\n", diffState(servers("v1", "s1", "s2", "s3", "s4", "s5", "s6", "s7")...))
	applyRefusedWith(t, f, changedMessage+": it adds ArtifactServer/s1, ArtifactServer/s2, ArtifactServer/s3, ArtifactServer/s4, ArtifactServer/s5, and 2 more objects", namedRemedy)
}

func TestANamedChangeNamesAtMostThreeFieldsOfAnObject(t *testing.T) {
	for _, test := range []struct {
		fields []string
		want   string
	}{
		{[]string{"spec.a"}, "it changes Machine/m (spec.a)"},
		{[]string{"spec.a", "spec.b", "spec.c"}, "it changes Machine/m (spec.a, spec.b and spec.c)"},
		{[]string{"spec.a", "spec.b", "spec.c", "spec.d"}, "it changes Machine/m (spec.a, spec.b and 2 more fields)"},
	} {
		if got := describeChanges([]objectChange{{identity: "Machine/m", change: reconciliation.Changed, fields: test.fields}}); got != test.want {
			t.Errorf("describeChanges(%q) = %q, want %q", test.fields, got, test.want)
		}
	}
	removed := []objectChange{{identity: "Machine/x", change: reconciliation.Removed}, {identity: "Machine/y", change: reconciliation.Added}}
	if got, want := describeChanges(removed), "it adds Machine/y and removes Machine/x"; got != want {
		t.Errorf("describeChanges = %q, want %q", got, want)
	}
	six := slices.Repeat([]objectChange{{identity: "Machine/x", change: reconciliation.Removed}}, 6)
	if got := describeChanges(six); !strings.HasSuffix(got, ", and 1 more object") {
		t.Errorf("describeChanges of six = %q", got)
	}
}

func TestABytesOnlyEditStillRefusesAndSaysSo(t *testing.T) {
	f := appliedOver(t, diffState(pool("a", "3")))
	f.publish("rev-2", "kind: Environment\n# a comment\n", diffState(pool("a", "3")))
	applyRefusedWith(t, f, changedMessage+": its bytes changed, but it declares the same objects", namedRemedy)
}

func TestRestoredBytesUnderANewRevisionStillRefuseAndSaySo(t *testing.T) {
	f := appliedOver(t, diffState(pool("a", "3")))
	f.update("rev-2", f.workspace.inputs)
	applyRefusedWith(t, f, changedMessage+": its input revision changed, though its bytes repeat the ones this apply froze", namedRemedy)
}

func TestAnUnreadableFrozenRevisionRefusesWithTheGeneralMessage(t *testing.T) {
	for name, lose := range map[string]func(diffFixture){
		"a collected revision": func(f diffFixture) { delete(f.workspace.revisions, testRevision) },
		"moved bytes": func(f diffFixture) {
			f.workspace.revisions[testRevision] = diffSources("kind: Environment\n# moved\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := appliedOver(t, diffState(pool("a", "3")))
			f.publish("rev-2", "kind: Environment\n# edited\n", diffState(pool("a", "2")))
			lose(f)
			applyRefusedWith(t, f, changedMessage, changedRemedy)
		})
	}
}

func TestAnUncompilableEditRefusesWithTheGeneralMessage(t *testing.T) {
	t.Run("the edit", func(t *testing.T) {
		f := appliedOver(t, diffState(pool("a", "3")))
		f.publish("rev-2", "kind: Environment\n# edited\n", nil)
		delete(f.compiler.states, digestOf(f.workspace.inputs))
		applyRefusedWith(t, f, changedMessage, changedRemedy)
	})
	t.Run("the frozen revision", func(t *testing.T) {
		f := appliedOver(t, diffState(pool("a", "3")))
		frozen := digestOf(f.workspace.inputs)
		f.publish("rev-2", "kind: Environment\n# edited\n", diffState(pool("a", "2")))
		delete(f.compiler.states, frozen)
		applyRefusedWith(t, f, changedMessage, changedRemedy)
	})
}

// continuationRefusal is what an apply over the failed apply, and the re-proof
// status reads, each report.
func continuationRefusal(t *testing.T, h *harness) (applied, recorded []diagnostics.Diagnostic) {
	t.Helper()
	ctx := context.Background()
	_, err := apply(h)
	operation, _ := durableOperation(t, h)
	var proof error
	if err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		proof = h.service.recordedRefusal(view, operation, sameBlocksProof{proved: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, statusErr := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if statusErr != nil || slices.ContainsFunc(status.NextSteps, func(step string) bool { return strings.HasPrefix(step, "bootwright apply") }) {
		t.Fatalf("status offered %+v (%v) over a refused continuation", status, statusErr)
	}
	return diagnostics.Of(err), diagnostics.Of(proof)
}

func requireContinuationRefusal(t *testing.T, h *harness, message string) {
	t.Helper()
	const remedy = "restore the exact input revision this operation froze"
	applied, recorded := continuationRefusal(t, h)
	for path, reported := range map[string][]diagnostics.Diagnostic{"apply": applied, "status": recorded} {
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != remedy {
			t.Errorf("the %s path reported %+v, want %q", path, reported, message)
		}
	}
}

func TestAContinuationUnderAnotherRevisionRefusesWithItsMessage(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	h.update("rev-2", h.workspace.inputs)
	requireContinuationRefusal(t, h, "the context input changed after this operation registered")
}

func TestAContinuationUnderOtherBytesRefusesWithItsMessage(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	h.workspace.inputs = diffSources("kind: Environment\n# edited\n")
	requireContinuationRefusal(t, h, "the frozen input no longer matches this operation")
}

func TestEveryChangedInputOverACompletedApplyRefuses(t *testing.T) {
	cases := stateMachineRows()["apply done"].cases
	for name, want := range map[string]string{
		"a completed apply under an input that adds an object":                              changedMessage + ": it adds ArtifactServer/delta",
		"a completed apply under an input that changes an object":                           changedMessage + ": it changes ArtifactServer/charlie (spec.image)",
		"a completed apply under an input that removes an object":                           changedMessage + ": it removes ArtifactServer/charlie",
		"a completed apply under an input whose bytes changed but declare the same objects": changedMessage + ": its bytes changed, but it declares the same objects",
		"a completed apply under an input that no longer compiles":                          changedMessage,
		"a completed apply under another build":                                             changedMessage + ": it removes ArtifactServer/charlie",
	} {
		t.Run(name, func(t *testing.T) {
			test, found := cases[name]
			if !found {
				t.Fatalf("the state machine declares no case %q", name)
			}
			h := newPlannedHarness(t, chainedDefinitions())
			test.arrange(t, h)
			before := countWrites(h)
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != want {
				t.Fatalf("the apply reported %+v (%v), want %q", reported, err, want)
			}
			assertNothingWritten(t, h, before)
		})
	}
}

func TestACanceledNamingReturnsTheCancellation(t *testing.T) {
	f := appliedOver(t, diffState(pool("a", "3")))
	f.publish("rev-2", "kind: Environment\n# edited\n", diffState(pool("a", "2")))
	operation, _ := durableOperation(t, f.harness)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	var refused error
	if err := f.workspace.ReadLifecycle(context.Background(), testContextName, func(view View) error {
		refused = f.service.changedInput(canceled, view, operation)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(refused, context.Canceled) {
		t.Fatalf("a canceled naming returned %v", refused)
	}
}
