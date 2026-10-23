package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type removalRecorder struct {
	*testCapability
	mutex sync.Mutex
	seen  []Execution
}

func (r *removalRecorder) Destroy(ctx context.Context, execution Execution) (Result, error) {
	r.mutex.Lock()
	r.seen = append(r.seen, execution)
	r.mutex.Unlock()
	if execution.Prepare != nil || execution.PrepareRemoval == nil {
		return Result{}, errors.New("removal has the wrong preparation authority")
	}
	if err := execution.PrepareRemoval(ctx, json.RawMessage(`{"receipt":"verified"}`)); err != nil {
		return Result{}, err
	}
	return r.testCapability.Destroy(ctx, execution)
}

func (r *removalRecorder) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	r.mutex.Lock()
	r.seen = append(r.seen, execution)
	r.mutex.Unlock()
	if execution.Prepare != nil || execution.PrepareRemoval != nil {
		return Observation{}, errors.New("read-only removal gained write authority")
	}
	return r.testCapability.ObserveRemoval(ctx, execution)
}

func TestRemovalReceiptIsDurableBeforeContinuationAndReadOnlyOnResolution(t *testing.T) {
	h := newHarness(t, "alpha")
	r := &removalRecorder{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: r}
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("unknown removal completed")
	}
	store := operationstore.New(h.workspace.area, time.Now)
	index, err := store.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.Attempt(context.Background(), index.Current, "alpha", 1)
	if err != nil || string(attempt.Preparation) != `{"receipt":"verified"}` {
		t.Fatalf("receipt was not durably recorded: %+v %v", attempt, err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	resolved, retried := false, false
	for _, execution := range r.seen {
		if execution.Resolution > 0 {
			resolved = true
			if len(execution.RemovalPreparations) != 1 || string(execution.RemovalPreparations[0]) != `{"receipt":"verified"}` {
				t.Fatalf("resolution lost its receipt: %+v", execution.RemovalPreparations)
			}
		}
		if execution.Attempt == 2 && execution.Resolution == 0 {
			retried = true
			if len(execution.RemovalPreparations) != 1 {
				t.Fatal("retry lost the original receipt")
			}
		}
	}
	if !resolved || !retried {
		t.Fatalf("resolution=%v retry=%v", resolved, retried)
	}
}

func TestRemovalProofsLendOnlyCompletedTransitiveDependenciesPairedWithApply(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "bravo"), definition("sibling")})
	h.capability.outcomeFor = map[string]Result{}
	for _, id := range []string{"alpha", "bravo", "charlie", "sibling"} {
		h.capability.outcomeFor[id] = Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"apply":"` + id + `"}`)}
	}
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":true}`)}, {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":true}`)}, {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":true}`)}, {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":true}`)}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, execution := range h.capability.executions {
		if execution.Block.ID != "alpha" || execution.ApplyOperation == execution.Operation {
			continue
		}
		ids := []string{}
		for _, pair := range execution.RemovalProved {
			ids = append(ids, pair.Removed.Block.ID)
			if pair.Removed.Block.ID != pair.Applied.Block.ID || pair.Removed.Verb != reconciliation.Destroy || pair.Applied.Verb != reconciliation.Apply || pair.Removed.State != reconciliation.BlockDone || pair.Applied.State != reconciliation.BlockDone || len(pair.Applied.Evidence) == 0 {
				t.Fatalf("unpaired or unproved dependency: %+v", pair)
			}
		}
		slices.Sort(ids)
		if !slices.Equal(ids, []string{"bravo", "charlie"}) {
			t.Fatalf("wrong removal closure, including a sibling or missing a dependency: %v", ids)
		}
		found = true
	}
	if !found {
		t.Fatal("no terminal inverse received its dependency receipts")
	}
}

func TestOriginalRemovalDependentsIncludesPositivelyPendingDescendants(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "bravo"), definition("sibling")})
	h.capability.errorFor = map[string]error{"bravo": errors.New("stopped before charlie")}
	if err := applyOnce(h); err == nil {
		t.Fatal("failed apply completed")
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, execution := range h.capability.executions {
		if execution.Block.ID != "alpha" || execution.ApplyOperation == execution.Operation {
			continue
		}
		ids := []string{}
		for _, original := range execution.OriginalRemovalDependents {
			ids = append(ids, original.Block.ID)
			if original.Block.ID == "charlie" && (original.State != reconciliation.BlockPending || len(original.Evidence) != 0 || original.Verb != reconciliation.Apply) {
				t.Fatalf("an unstarted descendant was not positively pending: %+v", original)
			}
		}
		if !slices.Equal(ids, []string{"bravo", "charlie"}) {
			t.Fatalf("pending original dependency missing or sibling leaked: %v", ids)
		}
		found = true
	}
	if !found {
		t.Fatal("no original dependent projection")
	}
}
