package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestEvidenceReportsWhatACompletedAttemptProved(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	found, err := h.service.Evidence(ctx, "lab", "ArtifactServer", "artifact-server-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("evidence = %+v", found)
	}
	proved := found[0]
	if proved.Kind != "ArtifactServer" || proved.Object != "artifact-server-lab" {
		t.Fatalf("identity = %+v", proved)
	}
	if proved.Implementation != "artifact-server-nginx-v1" || proved.Verb != reconciliation.Apply {
		t.Fatalf("attribution = %+v", proved)
	}
	if proved.State != reconciliation.BlockDone || string(proved.Evidence) != `{"ok":true}` {
		t.Fatalf("proof = %+v", proved)
	}
}

// An object no frozen block names has no entry: the absence of a record is not
// evidence that nothing was realized.
func TestEvidenceReportsNothingForAnObjectNoBlockNames(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ kind, object string }{
		{"Machine", "artifact-server-lab"},
		{"ArtifactServer", "absent"},
	} {
		found, err := h.service.Evidence(ctx, "lab", test.kind, test.object)
		if err != nil || len(found) != 0 {
			t.Fatalf("%s/%s = %+v (%v)", test.kind, test.object, found, err)
		}
	}
}

func TestEvidenceReportsNothingBeforeAnOperationExists(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	found, err := h.service.Evidence(context.Background(), "lab", "ArtifactServer", "artifact-server-lab")
	if err != nil || len(found) != 0 {
		t.Fatalf("evidence = %+v (%v)", found, err)
	}
}

// A block whose last attempt failed still carries what that attempt observed,
// and reports the state an operator must act on rather than a settled one.
func TestEvidenceCarriesAFailedAttemptWithItsState(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed, Evidence: json.RawMessage(`{"ok":false}`)}}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failing capability produced a successful apply")
	}
	found, err := h.service.Evidence(ctx, "lab", "ArtifactServer", "artifact-server-lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].State != reconciliation.BlockFailed {
		t.Fatalf("evidence = %+v", found)
	}
	if string(found[0].Evidence) != `{"ok":false}` {
		t.Fatalf("proof = %q", found[0].Evidence)
	}
}

// settlingResolution reads the last resolution of a block's first attempt
// through the store's own reader.
func settlingResolution(t *testing.T, h *harness, block string) operationstore.Attempt {
	t.Helper()
	record, found, err := operationstore.New(h.workspace.area, time.Now).LastResolution(context.Background(), firstOperation, block, 1)
	if err != nil || !found {
		t.Fatalf("the resolution of %s cannot be read (%t, %v)", block, found, err)
	}
	return record
}

// A block a resolution completed reads back what that resolution observed and
// the outcome its capability proved, whether the attempt it resolved never
// recorded its outcome or recorded it unknown beside evidence of its own: the
// resolution, not the attempt, is what proved the block. Only a completion
// whose outcome the capability could not tell reads back changed.
func TestEvidenceReportsWhatAResolutionProved(t *testing.T) {
	const block = "artifact-server-lab"
	for name, tc := range map[string]struct {
		interrupt bool
		proved    reconciliation.Outcome
		recorded  reconciliation.Outcome
	}{
		"an attempt that never recorded its outcome": {interrupt: true, proved: reconciliation.OutcomeUnchanged, recorded: reconciliation.OutcomeUnchanged},
		"an attempt whose outcome was unknown":       {proved: reconciliation.OutcomeUnchanged, recorded: reconciliation.OutcomeUnchanged},
		"a proved change":                            {proved: reconciliation.OutcomeChanged, recorded: reconciliation.OutcomeChanged},
		"an outcome the capability could not tell":   {recorded: reconciliation.OutcomeChanged},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t, block)
			if tc.interrupt {
				recorded := interruptCompletion(h, block)
				if err := applyOnce(h); err == nil {
					t.Fatal("an apply whose block could not record its completion succeeded")
				}
				clearFault(h, recorded)
			} else {
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown, Evidence: json.RawMessage(`{"attempted":true}`)}}
				if err := applyOnce(h); err == nil {
					t.Fatal("an apply whose outcome is unknown succeeded")
				}
			}
			h.capability.observations = []Observation{{
				Effect: reconciliation.EffectCompleted, Outcome: tc.proved, Evidence: json.RawMessage(`{"observed":true}`),
			}}
			if err := applyOnce(h); err != nil {
				t.Fatalf("continuing the apply: %v", err)
			}
			found, err := h.service.Evidence(ctx, "lab", "ArtifactServer", block)
			if err != nil || len(found) != 1 {
				t.Fatalf("evidence = %+v (%v)", found, err)
			}
			if found[0].State != reconciliation.BlockDone || string(found[0].Evidence) != `{"observed":true}` {
				t.Fatalf("the resolved block read back %s with %q", found[0].State, found[0].Evidence)
			}
			record := settlingResolution(t, h, block)
			if record.Phase != "observed" || record.Outcome != tc.recorded || record.Effect != reconciliation.EffectCompleted {
				t.Fatalf("the resolution recorded %s/%s/%s, want the %s its capability proved", record.Phase, record.Outcome, record.Effect, tc.recorded)
			}
		})
	}
}

// Evidence is read from the record that last settled a block. A resolution
// that proved nothing still carries what it observed, beside the unknown state
// an operator must act on, and one whose completion was never recorded carries
// nothing rather than the bytes of the resolution before it.
func TestEvidenceOfAResolutionStillRunningIsNone(t *testing.T) {
	const block = "artifact-server-lab"
	ctx := context.Background()
	h := newHarness(t, block)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown, Evidence: json.RawMessage(`{"attempted":true}`)}}
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose outcome is unknown succeeded")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown, Evidence: json.RawMessage(`{"first":true}`)}}
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose resolution proved nothing succeeded")
	}
	found, err := h.service.Evidence(ctx, "lab", "ArtifactServer", block)
	if err != nil || len(found) != 1 || found[0].State != reconciliation.BlockUnknown || string(found[0].Evidence) != `{"first":true}` {
		t.Fatalf("after an unproved resolution the evidence = %+v (%v)", found, err)
	}
	second := "replace " + path.Join(firstOperation, "blocks", block, "attempt-000001-resolution-000002.json")
	h.workspace.area.mutex.Lock()
	h.workspace.area.fail[second] = errors.New("interrupted")
	h.workspace.area.mutex.Unlock()
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"second":true}`)}}
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose resolution could not record its completion succeeded")
	}
	clearFault(h, second)
	found, err = h.service.Evidence(ctx, "lab", "ArtifactServer", block)
	if err != nil || len(found) != 1 || found[0].State != reconciliation.BlockUnknown || found[0].Evidence != nil {
		t.Fatalf("beside a resolution still running the evidence = %+v (%v)", found, err)
	}
}

func TestEvidenceRefusesAnUnnamedObject(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Evidence(context.Background(), "lab", "", "artifact-server-lab"); err == nil {
		t.Fatal("an unnamed kind was accepted")
	}
	if _, err := h.service.Evidence(context.Background(), "lab", "ArtifactServer", ""); err == nil {
		t.Fatal("an unnamed object was accepted")
	}
}

func TestWithMaterialOpensExactlyTheRequestedSecretsAndReleasesThem(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	var seen map[string]secrets.Material
	err := h.service.WithMaterial(ctx, MaterialRequest{ContextName: "lab", Secrets: []string{"artifact-server-tls"}},
		func(_ context.Context, material map[string]secrets.Material) error {
			seen = material
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("material = %+v", seen)
	}
	if len(h.binder.bound) != 1 || h.binder.bound[0] != "artifact-server-tls" {
		t.Fatalf("bound = %v", h.binder.bound)
	}
	if len(h.binder.released) != 1 {
		t.Fatalf("released = %v", h.binder.released)
	}
	// A bounded material read is not an operation: it registers none and takes
	// no store lock, so nothing about the context's records may have moved.
	if h.workspace.mutations != 0 || h.workspace.runs != 0 {
		t.Fatalf("mutations = %d runs = %d", h.workspace.mutations, h.workspace.runs)
	}
}

func TestWithMaterialReleasesWhatItOpenedWhenTheConsumerFails(t *testing.T) {
	refused := errors.New("refused")
	h := newHarness(t, "artifact-server-lab")
	err := h.service.WithMaterial(context.Background(),
		MaterialRequest{ContextName: "lab", Secrets: []string{"artifact-server-tls"}},
		func(context.Context, map[string]secrets.Material) error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	if len(h.binder.released) != 1 {
		t.Fatalf("released = %v", h.binder.released)
	}
}

func TestWithMaterialNeedsNoBindingWhenNothingIsRequested(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	called := false
	err := h.service.WithMaterial(context.Background(), MaterialRequest{ContextName: "lab"},
		func(_ context.Context, material map[string]secrets.Material) error {
			called = true
			if len(material) != 0 {
				t.Fatalf("material = %+v", material)
			}
			return nil
		})
	if err != nil || !called {
		t.Fatalf("call = %v called=%t", err, called)
	}
	if len(h.binder.bound) != 0 || len(h.binder.released) != 0 {
		t.Fatalf("bound = %v released = %v", h.binder.bound, h.binder.released)
	}
}

func TestWithMaterialRefusesARequestCarryingNoConsumer(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if err := h.service.WithMaterial(context.Background(), MaterialRequest{ContextName: "lab"}, nil); err == nil {
		t.Fatal("a request with no consumer was accepted")
	}
}
