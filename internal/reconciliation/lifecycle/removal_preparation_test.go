package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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
	t.Parallel()
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
	store := operationstore.New(h.workspace.area, h.workspace.logArea, time.Now)
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
	t.Parallel()
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
	t.Parallel()
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

func removed(id string) Result {
	return Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"removed":"` + id + `"}`)}
}

func TestAReplacementLendsWhatEachRemovalItReplacesProvedNearestFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "bravo"), dependent("delta", "charlie")})
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	var chain []operationstore.Operation
	for _, outcomes := range [][]Result{{removed("delta"), {Outcome: reconciliation.OutcomeFailed}}, {removed("charlie"), {Outcome: reconciliation.OutcomeFailed}}} {
		h.capability.outcomes = outcomes
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
			t.Fatal("the seeded removal failure did not fire")
		}
		failed, _ := durableOperation(t, h)
		chain = append(chain, failed)
	}
	h.capability.outcomes = []Result{removed("bravo"), removed("alpha")}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	replacement, _ := durableOperation(t, h)
	if replacement.Replaces != chain[1].ID || chain[1].Replaces != chain[0].ID {
		t.Fatalf("the replacement chain %s -> %s -> %s is not the removals' own", replacement.Replaces, chain[1].Replaces, chain[0].ID)
	}
	found := false
	for _, execution := range h.capability.executions {
		if execution.Block.ID != "alpha" || execution.ApplyOperation == execution.Operation {
			continue
		}
		proved := map[string]string{}
		for _, pair := range execution.RemovalProved {
			if _, twice := proved[pair.Removed.Block.ID]; twice || pair.Removed.Block.ID != pair.Applied.Block.ID || pair.Removed.State != reconciliation.BlockDone || pair.Applied.State != reconciliation.BlockDone || pair.Applied.Operation != execution.ApplyOperation {
				t.Fatalf("unpaired, unproved or repeated dependency: %+v", pair)
			}
			proved[pair.Removed.Block.ID] = pair.Removed.Operation + " " + string(pair.Removed.Evidence)
		}
		want := map[string]string{
			"bravo":   execution.Operation + ` {"removed":"bravo"}`,
			"charlie": chain[1].ID + ` {"removed":"charlie"}`,
			"delta":   chain[0].ID + ` {"removed":"delta"}`,
		}
		if len(proved) != len(want) || proved["bravo"] != want["bravo"] || proved["charlie"] != want["charlie"] || proved["delta"] != want["delta"] {
			t.Fatalf("the replacement lent %v, want %v", proved, want)
		}
		found = true
	}
	if !found {
		t.Fatal("the replacement never removed alpha")
	}
	records := operationstore.New(h.workspace.area, h.workspace.logArea, time.Now)
	plan, err := records.ReadPlan(ctx, replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := plan.Block("alpha")
	foreign, apply, cycle := chain[0], chain[0], chain[0]
	foreign.Source = "op-" + strings.Repeat("e1", 16)
	apply.Verb = reconciliation.Apply
	cycle.Replaces = chain[1].ID
	for _, test := range []struct {
		name    string
		record  operationstore.Operation
		message string
	}{
		{"foreign source", foreign, "is no removal of the same apply"},
		{"apply hop", apply, "is no removal of the same apply"},
		{"cycle", cycle, "name one another in a cycle"},
	} {
		store := &sourceStore{recordReader: records, operations: map[string]operationstore.Operation{chain[0].ID: test.record}}
		_, _, err := removalRecords(ctx, store, replacement, plan, alpha, false)
		t.Run(test.name, func(t *testing.T) { requireStateRefusal(t, err, test.message) })
	}
}
