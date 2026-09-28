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
// records hold a block that is not done refuses instead of removing only the
// done rest: a lost record reads back as pending, and skipping it would leave
// that block's effect on the host and then release the binding it needs. The
// refusal names every such block and leaves every record and binding as it was.
func TestADestroyAfterACompletedApplyWithABlockNotDoneRefuses(t *testing.T) {
	for name, test := range map[string]struct {
		lost       []string
		rewritten  map[string]reconciliation.BlockState
		unfinished []string
		done       []string
	}{
		"one block record lost": {
			lost:       []string{"bravo"},
			unfinished: []string{"bravo (pending)"},
			done:       []string{"alpha", "charlie"},
		},
		"two block records lost": {
			lost:       []string{"alpha", "charlie"},
			unfinished: []string{"alpha (pending)", "charlie (pending)"},
			done:       []string{"bravo"},
		},
		"a block record that reads failed": {
			rewritten:  map[string]reconciliation.BlockState{"charlie": reconciliation.BlockFailed},
			unfinished: []string{"charlie (failed)"},
			done:       []string{"alpha", "bravo"},
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
			bound, binds, presented, mutations, opened := slices.Clone(h.binder.bound), h.workspace.binds, len(h.presenter.presented), h.workspace.mutations, h.workspace.opened
			h.capability.destroys, h.capability.probes, h.capability.observes, h.capability.removals = nil, nil, nil, nil

			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if len(reported) == 0 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("destroy = %+v (%v)", reported, err)
			}
			message := reported[0].Message
			if !strings.Contains(message, current) || !strings.HasSuffix(message, ": "+strings.Join(test.unfinished, ", ")) {
				t.Fatalf("the refusal did not name the apply and each block that is not done: %q", message)
			}
			for _, block := range test.done {
				if strings.Contains(message, block) {
					t.Fatalf("the refusal named the done block %s: %q", block, message)
				}
			}
			if !strings.Contains(reported[0].Remediation, "bootwright status") {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
			// Nothing was planned, presented, bound, locked, probed or removed.
			if len(h.capability.removals) != 0 || len(h.presenter.presented) != presented {
				t.Fatalf("the refusal planned a removal: removals=%v presented=%d", h.capability.removals, len(h.presenter.presented)-presented)
			}
			if len(h.capability.destroys) != 0 || len(h.capability.probes) != 0 || len(h.capability.observes) != 0 {
				t.Fatalf("the refusal reached the host: destroys=%v probes=%v observes=%v",
					h.capability.destroys, h.capability.probes, h.capability.observes)
			}
			if h.workspace.mutations != mutations || h.workspace.opened != opened {
				t.Fatalf("the refusal took the exclusive lock or opened a bundle: mutations=%d opened=%d",
					h.workspace.mutations-mutations, h.workspace.opened-opened)
			}
			// Every record and binding is exactly as the apply and the loss left it.
			if !sameFiles(h.workspace.area, records) || currentOperation(t, h) != current {
				t.Fatal("the refusal changed durable operation state")
			}
			if !slices.Equal(h.binder.bound, bound) || len(h.binder.released) != 0 || h.workspace.binds != binds {
				t.Fatalf("the refusal bound %v, released %v or bound the controller %d times",
					h.binder.bound, h.binder.released, h.workspace.binds-binds)
			}
		})
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
			bound, binds, presented, mutations, opened := slices.Clone(h.binder.bound), h.workspace.binds, len(h.presenter.presented), h.workspace.mutations, h.workspace.opened

			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if result != nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("repeated apply = %+v, %+v (%v)", result, reported, err)
			}
			want := "the completed apply " + current + " records no block completion for these blocks, so this input cannot be proved applied: " + strings.Join(test.unfinished, ", ")
			if reported[0].Message != want {
				t.Fatalf("refusal = %q, want %q", reported[0].Message, want)
			}
			if reported[0].Remediation != "review its durable state with bootwright status" {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
			if !slices.Equal(h.capability.applies, applies) || len(h.presenter.presented) != presented {
				t.Fatalf("the refusal ran or presented an apply: applies=%v presented=%d", h.capability.applies, len(h.presenter.presented)-presented)
			}
			if h.workspace.mutations != mutations || h.workspace.opened != opened {
				t.Fatalf("the refusal took the exclusive lock or opened a bundle: mutations=%d opened=%d",
					h.workspace.mutations-mutations, h.workspace.opened-opened)
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
