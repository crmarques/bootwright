package lifecycle

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestApplyAdmissionRefusesBeforeClaimBindingAndNativeEffects(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	refused := errors.New("capacity unknown")
	calls := 0
	h.service.options.ApplyAdmission = func(_ context.Context, name string, catalog api.Catalog, owned map[string]ObjectOwnership, _ []reconciliation.Block) error {
		calls++
		if name != testContextName || len(catalog.OfKind(api.Environment)) != 1 || len(owned) != 0 {
			t.Fatal("admission did not receive the fresh locked snapshot")
		}
		return refused
	}
	if _, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("pure planning performed an external admission probe")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); !errors.Is(err, refused) {
		t.Fatalf("apply = %v", err)
	}
	if calls != 1 || len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 || len(h.capability.applies) != 0 {
		t.Fatal("refused admission claimed, bound or performed an effect")
	}
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership, []reconciliation.Block) error {
		return nil
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership, []reconciliation.Block) error {
		t.Fatal("settled apply or destroy ran admission")
		return refused
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRechecksAdmissionUnderTheExclusiveTransaction(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	refused := errors.New("capacity changed")
	calls := 0
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership, []reconciliation.Block) error {
		calls++
		if calls == 1 {
			return nil
		}
		return refused
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); !errors.Is(err, refused) {
		t.Fatalf("apply = %v", err)
	}
	if calls != 2 || len(h.capability.applies) != 0 {
		t.Fatalf("admission calls=%d, effects=%v", calls, h.capability.applies)
	}
	index, err := h.service.options.Operations(h.workspace.area).Index(context.Background())
	if err != nil || index.Current != "" {
		t.Fatalf("refused capacity registered operation: %+v, %v", index, err)
	}
}

func TestApplyAdmissionReceivesOnlyTheBlocksTheTransitionMayStart(t *testing.T) {
	alpha := definition("alpha")
	bravo := definition("bravo")
	bravo.Stage = reconciliation.StageMachines
	record := func(h *harness) *[][]string {
		seen := &[][]string{}
		h.service.options.ApplyAdmission = func(_ context.Context, _ string, _ api.Catalog, _ map[string]ObjectOwnership, starting []reconciliation.Block) error {
			ids := []string{}
			for _, block := range starting {
				ids = append(ids, block.ID)
			}
			*seen = append(*seen, ids)
			return nil
		}
		return seen
	}
	full := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha, bravo})
	seen := record(full)
	if _, err := full.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || !slices.Equal((*seen)[0], []string{"alpha", "bravo"}) || !slices.Equal((*seen)[1], []string{"alpha", "bravo"}) {
		t.Fatalf("a fresh full apply admitted %v", *seen)
	}
	staged := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha, bravo})
	seen = record(staged)
	if _, err := staged.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Stages: []string{"infrastructure-services"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || !slices.Equal((*seen)[0], []string{"alpha"}) || !slices.Equal((*seen)[1], []string{"alpha"}) {
		t.Fatalf("a selection of infrastructure-services admitted %v", *seen)
	}
	*seen = nil
	if _, err := staged.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || !slices.Equal((*seen)[0], []string{"bravo"}) || !slices.Equal((*seen)[1], []string{"bravo"}) {
		t.Fatalf("a continuation whose alpha is done admitted %v", *seen)
	}
	unresolved := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha, bravo})
	unresolved.capability.outcomeFor = map[string]Result{"bravo": {Outcome: reconciliation.OutcomeUnknown}}
	seen = record(unresolved)
	if _, err := unresolved.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Stages: []string{"infrastructure-services"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := unresolved.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Stages: []string{"machines"}, SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	if _, states := durableOperation(t, unresolved); states["alpha"] != reconciliation.BlockDone || states["bravo"] != reconciliation.BlockUnknown {
		t.Fatalf("arranged states = %v", states)
	}
	*seen = nil
	unresolved.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	_, _ = unresolved.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Stages: []string{"infrastructure-services"}, SkipConfirmation: true})
	if len(*seen) != 2 || !slices.Equal((*seen)[0], []string{"bravo"}) || !slices.Equal((*seen)[1], []string{"bravo"}) {
		t.Fatalf("a selection of infrastructure-services beside an unknown Machine block admitted %v", *seen)
	}
}
