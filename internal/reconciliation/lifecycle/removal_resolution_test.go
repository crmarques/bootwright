package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves holds a resolution
// to the verb its operation froze. A destroy's unproved block is observed for
// what its removal proves, so a target the removal has not taken back is no
// removal: its block is failed and retried, never done over what it kept. An
// apply's block is observed for the apply's effect, and so is the one a fresh
// destroy resolves before it registers, because the operation it replaces is
// an apply.
func TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves(t *testing.T) {
	const block = "artifact-server-lab"
	ctx := context.Background()
	apply := ApplyRequest{ContextName: "lab", SkipConfirmation: true}
	destroy := DestroyRequest{ContextName: "lab", SkipConfirmation: true}
	for _, tc := range []struct {
		name     string
		effect   reconciliation.EffectState
		state    reconciliation.OperationState
		block    reconciliation.BlockState
		code     string
		message  string
		released []string
	}{
		{"a target still present is no removal", reconciliation.EffectNoEffect, reconciliation.OperationFailed, reconciliation.BlockFailed,
			"lifecycle.state", "the frozen effect was never performed", nil},
		{"a proved removal completes", reconciliation.EffectCompleted, reconciliation.OperationDone, reconciliation.BlockDone,
			"", "", []string{"bind-1"}},
		{"an unproved removal stays unknown", reconciliation.EffectUnknown, reconciliation.OperationUnknown, reconciliation.BlockUnknown,
			"lifecycle.unknown", "the outcome of " + block + " is still unknown: its observation proved neither the effect nor its absence", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, block)
			if _, err := h.service.Apply(ctx, apply); err != nil {
				t.Fatal(err)
			}
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if _, err := h.service.Destroy(ctx, destroy); err == nil {
				t.Fatal("the seeded unproved removal did not fire")
			}
			h.capability.calls = nil
			h.capability.observations = []Observation{{Effect: tc.effect}}
			result, err := h.service.Destroy(ctx, destroy)
			if !slices.Equal(h.capability.calls, []string{"observe-removal:" + block}) {
				t.Fatalf("a destroy's resolution asked %v, want only its removal observation", h.capability.calls)
			}
			if result == nil || result.Receipt.State != string(tc.state) || result.Receipt.Verb != string(reconciliation.Destroy) {
				t.Fatalf("resolved removal = %+v (%v), want a %s destroy", result, err, tc.state)
			}
			if firstCode(err) != tc.code || (tc.message != "" && !slices.ContainsFunc(diagnostics.Of(err), func(reported diagnostics.Diagnostic) bool {
				return reported.Message == tc.message
			})) {
				t.Fatalf("resolved removal reported %v, want %s %q", err, tc.code, tc.message)
			}
			if operation, blocks := durableOperation(t, h); operation.Verb != reconciliation.Destroy || blocks[block] != tc.block {
				t.Fatalf("the %s holds %s %s, want %s", operation.Verb, block, blocks[block], tc.block)
			}
			if len(h.capability.destroys) != 1 {
				t.Fatalf("the invocation that resolved the removal also ran it: %v", h.capability.destroys)
			}
			if !slices.Equal(h.binder.released, tc.released) {
				t.Fatalf("released bindings = %v, want %v", h.binder.released, tc.released)
			}
			if tc.effect != reconciliation.EffectNoEffect {
				return
			}
			retried, err := h.service.Destroy(ctx, destroy)
			if err != nil || retried.Receipt.State != string(reconciliation.OperationDone) || len(h.capability.destroys) != 2 {
				t.Fatalf("the removal never performed was not retried to completion: %+v (%v), destroys %v", retried, err, h.capability.destroys)
			}
			if !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("released bindings = %v", h.binder.released)
			}
		})
	}
	t.Run("an apply's resolution observes the apply", func(t *testing.T) {
		h := newHarness(t, block)
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		if _, err := h.service.Apply(ctx, apply); err == nil {
			t.Fatal("the seeded unproved apply did not fire")
		}
		h.capability.calls = nil
		h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
		result, err := h.service.Apply(ctx, apply)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("resolved apply = %+v (%v)", result, err)
		}
		if !slices.Equal(h.capability.calls, []string{"observe:" + block}) {
			t.Fatalf("an apply's resolution asked %v, want only its observation", h.capability.calls)
		}
	})
	t.Run("a fresh destroy resolves the apply it replaces as an apply", func(t *testing.T) {
		h := newHarness(t, block)
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		if _, err := h.service.Apply(ctx, apply); err == nil {
			t.Fatal("the seeded unproved apply did not fire")
		}
		h.capability.calls = nil
		h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
		result, err := h.service.Destroy(ctx, destroy)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
			t.Fatalf("removal over an unproved apply = %+v (%v)", result, err)
		}
		if !slices.Equal(h.capability.calls, []string{"observe:" + block}) {
			t.Fatalf("the replaced apply's resolution asked %v, want only its observation", h.capability.calls)
		}
		if !slices.Equal(h.capability.destroys, []string{block}) {
			t.Fatalf("destroyed blocks = %v", h.capability.destroys)
		}
	})
}
