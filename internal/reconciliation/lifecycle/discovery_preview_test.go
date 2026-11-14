package lifecycle

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The wrapper denies effects while retaining the production read-only ports.
type previewOnlyWorkspace struct {
	Workspace
	t *testing.T
}

func (w previewOnlyWorkspace) MutateLifecycle(context.Context, string, func(Transaction) error) error {
	w.t.Fatal("preview acquired a mutable workspace")
	return errors.New("preview mutation")
}

func (w previewOnlyWorkspace) RunLifecycle(context.Context, string, func(RunView) error) error {
	w.t.Fatal("preview acquired an execution workspace")
	return errors.New("preview execution")
}

func previewEffects(h *harness) []int {
	return []int{h.workspace.mutations, h.workspace.runs, len(h.capability.calls), len(h.capability.probed),
		len(h.binder.bound), len(h.binder.released), h.binder.issued, len(h.binder.journal),
		h.guard.entered(), h.confirmer.asked, len(h.presenter.presented), int(h.entropy[0])}
}

func TestDiscoveryPreviewNeverExecutesOrConfirmsAnyPreparationState(t *testing.T) {
	for _, phase := range []string{"fresh", "paused", "unknown", "prepared", "manual", "manual-only"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			h := automaticHarness(t)
			var first *OperationResult
			var err error
			switch phase {
			case "paused":
				first, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true})
			case "unknown", "manual", "manual-only":
				h.capability.outcomeFor = map[string]Result{"inventory": {Outcome: reconciliation.OutcomeUnknown}}
				if phase == "manual" || phase == "manual-only" {
					first, err = h.service.Prepare(ctx, requestPreparation())
				} else {
					first, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
				}
			case "prepared":
				first, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			}
			if phase != "fresh" && first == nil {
				t.Fatalf("fixture did not reach %s: %v", phase, err)
			}
			if phase == "manual-only" {
				h.service.options.Preparations[0].Automatic = nil
			}
			h.service.workspace = previewOnlyWorkspace{h.service.workspace, t}
			before := previewEffects(h)
			for range 2 {
				preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
				if err != nil || preview == nil || preview.Receipt.Verb != "plan" || preview.Receipt.State != "preview" {
					t.Fatalf("%s preview: %v %+v", phase, err, preview)
				}
				if phase == "manual" || phase == "manual-only" {
					if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
						t.Fatal("ordinary apply silently adopted manual preparation")
					}
				}
				if after := previewEffects(h); !slices.Equal(before, after) {
					t.Fatalf("preview performed effects: before %v, after %v", before, after)
				}
			}
		})
	}
}
