package lifecycle

import (
	"context"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A completed apply owns its whole frozen plan, so a destroy over one whose
// records hold a block that is not done takes back that whole plan rather than
// only the done rest (Q93): a lost record reads back as pending, and skipping
// it would leave that block's effect on the host and then release the binding
// it needs. An unproved block is resolved by observation before the removal
// registers, and the apply's own record stays done, so a removal that stops
// still covers the whole plan.
func TestADestroyAfterACompletedApplyWithABlockNotDoneTakesBackItsWholePlan(t *testing.T) {
	for name, test := range map[string]struct {
		lost      []string
		rewritten map[string]reconciliation.BlockState
		observed  []string
		effect    reconciliation.EffectState
	}{
		"one block record lost":     {lost: []string{"bravo"}},
		"two block records lost":    {lost: []string{"alpha", "charlie"}},
		"a block that reads failed": {rewritten: map[string]reconciliation.BlockState{"charlie": reconciliation.BlockFailed}},
		"a block that reads unknown": {
			rewritten: map[string]reconciliation.BlockState{"charlie": reconciliation.BlockUnknown},
			observed:  []string{"charlie"},
			effect:    reconciliation.EffectCompleted,
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha", "bravo", "charlie")
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			applied := currentOperation(t, h)
			h.workspace.area.mutex.Lock()
			for _, block := range test.lost {
				delete(h.workspace.area.files, path.Join(applied, "blocks", block, "state.json"))
			}
			h.workspace.area.mutex.Unlock()
			for block, state := range test.rewritten {
				rewriteState(t, h, path.Join(applied, "blocks", block, "state.json"), string(state))
			}
			h.capability.destroys, h.capability.observes = nil, nil
			if test.effect != "" {
				h.capability.observations = []Observation{{Effect: test.effect}}
			}

			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			if err != nil {
				t.Fatalf("destroy: %+v (%v)", diagnostics.Of(err), err)
			}
			if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Operation == applied {
				t.Fatalf("receipt = %+v", result.Receipt)
			}
			if removed := slices.Sorted(slices.Values(h.capability.destroys)); !slices.Equal(removed, []string{"alpha", "bravo", "charlie"}) {
				t.Fatalf("the removal took back %v, want the whole frozen plan", h.capability.destroys)
			}
			for _, block := range test.observed {
				if !slices.Contains(h.capability.observes, block) {
					t.Fatalf("the removal did not resolve %s before taking it back: observes=%v", block, h.capability.observes)
				}
			}
			if !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("released %v", h.binder.released)
			}
		})
	}
}

// A removal that cannot register after it tried to resolve a completed
// apply's block leaves the apply recorded done, so the next removal still
// covers its whole frozen plan rather than only what a failed or unknown
// apply's records show started.
func TestAnUnresolvedCompletedApplyKeepsItsRecordDone(t *testing.T) {
	h := newHarness(t, "alpha", "bravo", "charlie")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	applied := currentOperation(t, h)
	rewriteState(t, h, path.Join(applied, "blocks", "charlie", "state.json"), string(reconciliation.BlockUnknown))
	h.capability.destroys = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if firstCode(err) != "lifecycle.unknown" || len(h.capability.destroys) != 0 {
		t.Fatalf("destroy = %+v (%v), destroys = %v", diagnostics.Of(err), err, h.capability.destroys)
	}
	operation, states := durableOperation(t, h)
	if operation.ID != applied || operation.State != reconciliation.OperationDone || states["charlie"] != reconciliation.BlockUnknown {
		t.Fatalf("the completed apply now reads %s %s with %v", operation.ID, operation.State, states)
	}
}

// An apply of the unchanged input settles on its completed apply's word that
// every block is done. Records that hold a block that is not done contradict
// that, so the repeat refuses instead of reporting the input applied, names
// every such block, and settles, registers, locks and binds nothing.
func TestARepeatedApplyOverACompletedApplyWithABlockNotDoneRefuses(t *testing.T) {
	for name, test := range map[string]struct {
		lost       []string
		rewritten  map[string]reconciliation.BlockState
		unfinished []string
	}{
		"a lost block record": {
			lost:       []string{"bravo"},
			unfinished: []string{"bravo (pending)"},
		},
		"a block record that reads failed": {
			rewritten:  map[string]reconciliation.BlockState{"charlie": reconciliation.BlockFailed},
			unfinished: []string{"charlie (failed)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha", "bravo", "charlie")
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			current := currentOperation(t, h)
			h.workspace.area.mutex.Lock()
			for _, block := range test.lost {
				delete(h.workspace.area.files, path.Join(current, "blocks", block, "state.json"))
			}
			h.workspace.area.mutex.Unlock()
			for block, state := range test.rewritten {
				rewriteState(t, h, path.Join(current, "blocks", block, "state.json"), string(state))
			}
			records := h.workspace.area.clone()
			applies, evidence := slices.Clone(h.capability.applies), slices.Clone(h.workspace.evidence)
			bound, binds, presented, mutations, opened := slices.Clone(h.binder.bound), h.workspace.binds, len(h.presenter.presented), h.workspace.mutations, h.guard.entered()

			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if result != nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("repeated apply = %+v, %+v (%v)", result, reported, err)
			}
			want := "the completed apply " + current + " records no block completion for these blocks, so this input cannot be proved applied: " + strings.Join(test.unfinished, ", ")
			if reported[0].Message != want {
				t.Fatalf("refusal = %q, want %q", reported[0].Message, want)
			}
			if reported[0].Remediation != "take back its whole frozen plan with bootwright destroy --context lab, which first resolves each unproved block, or delete the context with bootwright context delete --name lab --purge --allow-orphans, which abandons what it may still own" {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
			if !slices.Equal(h.capability.applies, applies) || len(h.presenter.presented) != presented {
				t.Fatalf("the refusal ran or presented an apply: applies=%v presented=%d", h.capability.applies, len(h.presenter.presented)-presented)
			}
			if h.workspace.mutations != mutations || h.guard.entered() != opened {
				t.Fatalf("the refusal took the exclusive lock or launched the runtime: mutations=%d launches=%d",
					h.workspace.mutations-mutations, h.guard.entered()-opened)
			}
			if !sameFiles(h.workspace.area, records) || currentOperation(t, h) != current || !slices.Equal(h.workspace.evidence, evidence) {
				t.Fatal("the refusal changed durable operation state or its evidence")
			}
			if !slices.Equal(h.binder.bound, bound) || len(h.binder.released) != 0 || h.workspace.binds != binds {
				t.Fatalf("the refusal bound %v, released %v or bound the controller %d times",
					h.binder.bound, h.binder.released, h.workspace.binds-binds)
			}
		})
	}
}

// The same removal over an intact completed apply is not affected: every block
// of the frozen plan is done, so the removal takes back each one and releases
// the binding its apply froze.
func TestADestroyAfterAnIntactCompletedApplyRemovesEveryBlock(t *testing.T) {
	h := newHarness(t, "alpha", "bravo", "charlie")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	applied := currentOperation(t, h)
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Operation == applied {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if removed := slices.Sorted(slices.Values(h.capability.destroys)); !slices.Equal(removed, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("the removal took back %v", h.capability.destroys)
	}
	if !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("released %v", h.binder.released)
	}
}

// Every view of what a completed apply owns reports what its destroy takes
// back, the whole frozen plan, so a lost block record neither drops its object
// from the ownership a superseding removal keeps reporting, nor from the
// realizations status reads, nor from the objects a lost frozen binding names.
func TestOwnershipViewsOfACompletedApplyCoverABlockWhoseRecordIsLost(t *testing.T) {
	ctx := context.Background()
	identities := []string{"ArtifactServer/alpha", "ArtifactServer/bravo", "ArtifactServer/charlie"}
	t.Run("a superseding removal", func(t *testing.T) {
		h := newPlannedHarness(t, chainedDefinitions())
		completeApply(t, h)
		lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
		// The chain removes charlie, then bravo, then fails on alpha; the
		// superseding removal names alpha alone and fails again.
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("the seeded removal failure did not fire")
		}
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("the seeded superseding failure did not fire")
		}
		owned, err := h.service.Ownership(ctx, testContextName)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"ArtifactServer/alpha": "failed", "ArtifactServer/bravo": "done", "ArtifactServer/charlie": "done"}
		for identity, state := range want {
			if got, found := owned[identity]; !found || got.Verb != "destroy" || got.State != state {
				t.Fatalf("ownership of %s = %+v (%t), want a destroy reading %s in %+v", identity, got, found, state, owned)
			}
		}
		realized := realizedNow(t, h)
		for _, identity := range identities[1:] {
			if status, found := realized[identity]; !found || status != RealizationPending {
				t.Fatalf("realization of %s = %q (%t) in %+v, want pending", identity, status, found, realized)
			}
		}
	})
	t.Run("a lost frozen binding", func(t *testing.T) {
		h := lostBindingStates()["a completed apply"](t)
		lose(h, path.Join(currentOperation(t, h), "blocks", "charlie")+"/")
		deleteBinding(t, h)
		status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
		if err != nil {
			t.Fatal(err)
		}
		for _, identity := range identities {
			if !slices.ContainsFunc(status.Contradictions, func(entry string) bool {
				return strings.Contains(entry, "what that operation owns: ") && strings.Contains(entry, identity)
			}) {
				t.Fatalf("status names %q, want the lost binding to name %s", status.Contradictions, identity)
			}
		}
	})
}

// realizedNow is what status reads as the realization of each object now.
func realizedNow(t *testing.T, h *harness) map[string]RealizationStatus {
	t.Helper()
	var realized map[string]RealizationStatus
	err := h.service.workspace.ReadLifecycle(context.Background(), testContextName, func(view View) error {
		store := h.service.store(view)
		index, err := store.Index(context.Background())
		if err != nil {
			return err
		}
		operation, err := store.ReadOperation(context.Background(), index.Current)
		if err != nil {
			return err
		}
		plan, err := store.ReadPlan(context.Background(), operation.ID)
		if err != nil {
			return err
		}
		states, err := store.BlockStates(context.Background(), operation.ID, plan)
		if err != nil {
			return err
		}
		realized, err = realizations(context.Background(), store, operation, plan, states)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return realized
}
