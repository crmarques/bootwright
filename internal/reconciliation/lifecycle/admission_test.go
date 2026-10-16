package lifecycle

import (
	"context"
	"errors"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestApplyAdmissionRefusesBeforeClaimBindingAndNativeEffects(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	refused := errors.New("capacity unknown")
	calls := 0
	h.service.options.ApplyAdmission = func(_ context.Context, name string, catalog api.Catalog, owned map[string]ObjectOwnership) error {
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
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership) error { return nil }
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership) error {
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
	h.service.options.ApplyAdmission = func(context.Context, string, api.Catalog, map[string]ObjectOwnership) error {
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
