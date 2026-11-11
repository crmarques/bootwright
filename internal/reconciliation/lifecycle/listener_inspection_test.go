package lifecycle

import (
	"context"
	"errors"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestPublicInspectionReportsUnknownWithoutBlockingPurePreview(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	calls := 0
	h.service.options.PlanInspection = func(_ context.Context, name string, _ api.Catalog, full reconciliation.Plan, starting []reconciliation.Block) ([]PlanCheck, error) {
		calls++
		if name != testContextName || len(full.Blocks) != 1 || len(starting) != 1 {
			t.Fatal("inspection lost its complete plan and selection")
		}
		return []PlanCheck{{ID: "listener-firewall", Status: "unknown", Summary: "runtime firewall unavailable", Remedy: "verify manually"}}, nil
	}
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || calls != 1 || result.Checks[0].Status != "unknown" {
		t.Fatal(result, err, calls)
	}
	if len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 || len(h.capability.applies) != 0 {
		t.Fatal("public inspection created lifecycle effects")
	}
}

func TestListenerAdmissionRefusesBeforeRegistrationAndRechecksExclusive(t *testing.T) {
	for _, reject := range []int{1, 2} {
		h := newHarness(t, "artifact-server-lab")
		calls := 0
		refused := errors.New("listener firewall closed")
		h.service.options.ListenerAdmission = func(_ context.Context, name string, _ api.Catalog, plan reconciliation.Plan, starting []reconciliation.Block) error {
			calls++
			if name != testContextName || len(plan.Blocks) != 1 || len(starting) != 1 {
				t.Fatal("listener admission lost frozen plan")
			}
			if calls == reject {
				return refused
			}
			return nil
		}
		if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); !errors.Is(err, refused) {
			t.Fatal(err)
		}
		index, err := h.service.options.Operations(h.workspace.area, h.workspace.logArea).Index(context.Background())
		if err != nil || index.Current != "" || len(h.capability.applies) != 0 || calls != reject {
			t.Fatalf("index=%+v effects=%v calls=%d err=%v", index, h.capability.applies, calls, err)
		}
	}
}

func TestBlockAdmissionRefusesBeforeTheEffectAndCanBeRetried(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	refused := failure("preflight.failed", "live prerequisite disappeared", "restore the prerequisite and retry")
	calls := 0
	h.service.options.BlockAdmission = func(_ context.Context, plan reconciliation.Plan, block reconciliation.Block) error {
		calls++
		if len(plan.Blocks) != 1 || block.ID != "artifact-server-lab" {
			t.Fatal("lost frozen block")
		}
		return refused
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("live admission passed")
	} else if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "preflight.failed" {
		t.Fatal(reported)
	}
	if calls != 1 || len(h.capability.applies) != 0 {
		t.Fatal("effect crossed failed live admission")
	}
	h.service.options.BlockAdmission = nil
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.applies) != 1 {
		t.Fatal("refused pending block did not retry")
	}
}
