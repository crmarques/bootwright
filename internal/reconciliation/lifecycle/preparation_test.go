package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type preparedCall struct {
	call         string
	resolution   int
	prepares     bool
	preparations []string
}

// preparingRecorder publishes a before-state on the apply attempts prepareOn
// names and notes what every call was handed.
type preparingRecorder struct {
	*testCapability
	mutex     sync.Mutex
	prepareOn map[int]bool
	handed    []preparedCall
}

func (r *preparingRecorder) note(call string, execution Execution) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	var preparations []string
	for _, preparation := range execution.ApplyPreparations {
		preparations = append(preparations, string(preparation))
	}
	r.handed = append(r.handed, preparedCall{call: call + execution.Block.ID, resolution: execution.Resolution,
		prepares: execution.Prepare != nil, preparations: preparations})
}

func (r *preparingRecorder) Apply(ctx context.Context, execution Execution) (Result, error) {
	r.note("apply:", execution)
	if r.prepareOn[execution.Attempt] {
		if execution.Prepare == nil {
			return Result{Outcome: reconciliation.OutcomeFailed}, errors.New("the apply attempt cannot publish its before-state")
		}
		preparation := json.RawMessage(`{"attempt":` + strconv.Itoa(execution.Attempt) + `}`)
		if err := execution.Prepare(ctx, preparation); err != nil {
			return Result{Outcome: reconciliation.OutcomeFailed}, err
		}
	}
	return r.testCapability.Apply(ctx, execution)
}

func (r *preparingRecorder) Destroy(ctx context.Context, execution Execution) (Result, error) {
	r.note("destroy:", execution)
	return r.testCapability.Destroy(ctx, execution)
}

func (r *preparingRecorder) Observe(ctx context.Context, execution Execution) (Observation, error) {
	r.note("observe:", execution)
	return r.testCapability.Observe(ctx, execution)
}

func (r *preparingRecorder) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	r.note("observe-removal:", execution)
	return r.testCapability.ObserveRemoval(ctx, execution)
}

func (r *preparingRecorder) calls() []preparedCall {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.handed)
}

func recordingPreparations(h *harness, attempts ...int) *preparingRecorder {
	recorder := &preparingRecorder{testCapability: h.capability, prepareOn: map[int]bool{}}
	for _, attempt := range attempts {
		recorder.prepareOn[attempt] = true
	}
	h.service.capabilities = testResolver{capability: recorder}
	return recorder
}

func TestApplyAttemptRecordsItsPreparation(t *testing.T) {
	h := newHarness(t, "alpha")
	recordingPreparations(h, 1)
	if err := applyOnce(h); err != nil {
		t.Fatalf("an apply that published its before-state: %v", err)
	}
	attempt, err := operationstore.New(h.workspace.area, time.Now).Attempt(context.Background(), firstOperation, "alpha", 1)
	if err != nil || string(attempt.Preparation) != `{"attempt":1}` {
		t.Fatalf("the attempt recorded %s (%v)", attempt.Preparation, err)
	}
}

func TestRemovalReceivesItsOwnApplyPreparationsInAttemptOrder(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("bravo", "alpha")})
	recorder := recordingPreparations(h, 1, 3)
	h.capability.errorFor = map[string]error{"alpha": errors.New("the attempt failed")}
	for range 2 {
		if err := applyOnce(h); err == nil {
			t.Fatal("a failing attempt completed the apply")
		}
	}
	never, err := applyPreparations(context.Background(), operationstore.New(h.workspace.area, time.Now), firstOperation, "bravo")
	if err != nil || never != nil {
		t.Fatalf("a block the apply never started was handed %q (%v)", never, err)
	}
	h.capability.errorFor = nil
	if err := applyOnce(h); err != nil {
		t.Fatalf("the third attempt: %v", err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved removal completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("resolving the removal: %v", err)
	}
	want := []string{`{"attempt":1}`, `{"attempt":3}`}
	removals, resolutions := 0, 0
	for _, call := range recorder.calls() {
		switch call.call {
		case "destroy:bravo", "destroy:alpha":
			removals++
		case "observe-removal:bravo", "observe-removal:alpha":
			resolutions++
		default:
			continue
		}
		wanted := want
		if call.call[len(call.call)-5:] == "bravo" {
			wanted = []string{`{"attempt":1}`}
		}
		if !slices.Equal(call.preparations, wanted) {
			t.Fatalf("%s was handed %q, want %q", call.call, call.preparations, wanted)
		}
	}
	if removals == 0 || resolutions == 0 {
		t.Fatalf("the removal ran %d attempts and %d resolutions: %+v", removals, resolutions, recorder.calls())
	}
}

func TestOnlyAnApplysOwnAttemptMayPrepare(t *testing.T) {
	h := newHarness(t, "alpha")
	recorder := recordingPreparations(h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if err := applyOnce(h); err == nil {
		t.Fatal("an unproved effect completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	if err := applyOnce(h); err != nil {
		t.Fatalf("the resolving apply: %v", err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("the removal: %v", err)
	}
	applies := 0
	for _, call := range recorder.calls() {
		own := call.call == "apply:alpha"
		if own {
			applies++
		}
		if call.prepares != own {
			t.Fatalf("%s (resolution %d) may publish a before-state: %t", call.call, call.resolution, call.prepares)
		}
	}
	if applies != 2 || len(recorder.calls()) < 4 {
		t.Fatalf("calls = %+v", recorder.calls())
	}
}
