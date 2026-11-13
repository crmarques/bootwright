package lifecycle

import (
	"fmt"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestInvocationParallelismBoundsApplyAndDestroy(t *testing.T) {
	for _, verb := range []string{"apply", "destroy"} {
		for _, limit := range []int{0, 1, 12} {
			t.Run(fmt.Sprintf("%s/%d", verb, limit), func(t *testing.T) {
				var definitions []reconciliation.BlockDefinition
				for i := range 13 {
					definitions = append(definitions, definition(fmt.Sprintf("step-%02d", i)))
				}
				h := scheduled(t, 0, definitions...)
				if verb == "destroy" {
					if _, err := h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
						t.Fatal(err)
					}
				}
				watch := newOverlap(13)
				t.Cleanup(watch.open)
				if verb == "apply" {
					h.capability.hold, h.capability.released = watch.enter, watch.leave
				} else {
					h.capability.destroyHold = func(id string) { watch.enter(id); watch.leave(id) }
				}
				finished := make(chan error, 1)
				go func() {
					var err error
					if verb == "apply" {
						_, err = h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Parallelism: limit})
					} else {
						_, err = h.service.Destroy(t.Context(), DestroyRequest{ContextName: "lab", SkipConfirmation: true, Parallelism: limit})
					}
					finished <- err
				}()
				want := limit
				if want == 0 {
					want = MaxRunningBlocks
				}
				watch.gather(t, want)
				watch.open()
				if err := returnedWithin(t, finished); err != nil {
					t.Fatal(err)
				}
				if got := watch.highest(); got != want {
					t.Fatalf("peak=%d, want %d", got, want)
				}
				if h.service.Concurrency() != MaxRunningBlocks {
					t.Fatal("invocation override changed the shared service")
				}
			})
		}
	}
}

func TestNegativeParallelismRefusesBeforeLifecycleAccess(t *testing.T) {
	var service Service
	_, applyErr := service.Apply(t.Context(), ApplyRequest{Parallelism: -1})
	_, destroyErr := service.Destroy(t.Context(), DestroyRequest{Parallelism: -1})
	for _, err := range []error{applyErr, destroyErr} {
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "lifecycle.state" || found[0].Message != "parallelism must not be negative" {
			t.Fatalf("negative parallelism: %v", err)
		}
	}
}

func TestContinuationCanChangeParallelismWithoutChangingThePlan(t *testing.T) {
	first := definition("first")
	first.Stage = reconciliation.StageInfrastructureServices
	definitions := []reconciliation.BlockDefinition{first}
	for i := range 12 {
		next := dependent(fmt.Sprintf("next-%02d", i), "first")
		next.Stage = reconciliation.StageMachines
		definitions = append(definitions, next)
	}
	h := scheduled(t, 0, definitions...)
	paused, err := h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", Stages: []string{"infrastructure-services"}, SkipConfirmation: true, Parallelism: 1})
	if err != nil || paused.Receipt.State != "paused" {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	watch := newOverlap(12)
	t.Cleanup(watch.open)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	var continued *OperationResult
	go func() {
		var err error
		continued, err = h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Parallelism: 12})
		finished <- err
	}()
	watch.gather(t, 12)
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if continued.Receipt.Operation != paused.Receipt.Operation || continued.Receipt.State != "done" {
		t.Fatalf("continuation: %+v", continued.Receipt)
	}
	if h.presenter.presented[0].Bound != 1 || h.presenter.presented[1].Bound != 12 {
		t.Fatalf("presented bounds: %+v", h.presenter.presented)
	}
}

func TestParallelismOverrideKeepsThrottleAndExclusiveClaims(t *testing.T) {
	h := scheduled(t, 0, throttled("alpha", "group", "shared"), throttled("bravo", "group", "shared"), throttled("charlie", "group"), definition("independent"))
	h.service.options.Throttle = 2
	watch := watchThrottle(4, "alpha", "bravo", "charlie")
	t.Cleanup(watch.open)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Parallelism: 16})
		finished <- err
	}()
	watch.gather(t, 3)
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if watch.highestHolders() > 2 || watch.together("alpha", "bravo") {
		t.Fatal("override bypassed throttle or exclusive claims")
	}
}
