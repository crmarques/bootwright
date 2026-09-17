package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
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
