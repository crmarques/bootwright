package lifecycle

import (
	"context"
	"path"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Status offers a next step only where that verb's decision would pass over
// the records status read. An offered verb decides: it presents its plan and,
// declined, writes nothing, and a plan previewing it names the next action
// status names. Each verb status leaves out refuses lifecycle.state before it
// presents or writes anything, and so does a plan previewing that verb.
func TestStatusOffersOnlyTheVerbsTheRecordsAllow(t *testing.T) {
	ctx := context.Background()
	failed := map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}}
	for _, row := range []struct {
		name    string
		arrange func(*testing.T, *harness)
		steps   []string
		next    string
	}{
		{
			name: "a lost index",
			arrange: func(t *testing.T, h *harness) {
				completeApply(t, h)
				lose(h, "index.json")
			},
			steps: []string{},
		},
		{
			name: "a lost block record",
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, failed)
				lose(h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"))
			},
			steps: []string{}, next: "continue-apply",
		},
		{
			name: "a failed apply whose blocks are all done",
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, failed)
				rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"), string(reconciliation.BlockDone))
			},
			steps: []string{}, next: "continue-apply",
		},
		{
			name: "a dependency not done beside a dependent that started",
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, failed)
				rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", "bravo", "state.json"), string(reconciliation.BlockFailed))
			},
			steps: []string{"bootwright apply"}, next: "continue-apply",
		},
		{
			name: "a failed apply that started nothing",
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}})
				lose(h, path.Join(currentOperation(t, h), "blocks", "alpha")+"/")
			},
			steps: []string{"bootwright apply"}, next: "continue-apply",
		},
		{
			name: "a running destroy with a lost block record",
			arrange: func(t *testing.T, h *harness) {
				failedChainedRemoval(t, h)
				removal := currentOperation(t, h)
				rewriteState(t, h, path.Join(removal, "operation.json"), string(reconciliation.OperationRunning))
				lose(h, path.Join(removal, "blocks", "charlie", "state.json"))
			},
			steps: []string{}, next: "continue-destroy",
		},
		{
			name: "a failed destroy",
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, nil)
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
				result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
				if err == nil || result == nil || result.Receipt.State != "failed" || result.Receipt.Next != "destroy" {
					t.Fatalf("the failed removal = %+v (%v), want a receipt naming destroy", result, err)
				}
				h.capability.outcomes = nil
			},
			steps: []string{"bootwright destroy"}, next: "destroy",
		},
		{
			name: "a failed destroy with a lost block record",
			arrange: func(t *testing.T, h *harness) {
				failedChainedRemoval(t, h)
				lose(h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"))
			},
			steps: []string{"bootwright destroy"}, next: "destroy",
		},
		{
			// bravo reads unknown as a superseding removal killed once its
			// resolution landed, before it recorded what that left, leaves it.
			name: "a failed destroy holding an unknown block beside a lost block record",
			arrange: func(t *testing.T, h *harness) {
				failedChainedRemoval(t, h)
				removal := currentOperation(t, h)
				rewriteState(t, h, path.Join(removal, "blocks", "bravo", "state.json"), string(reconciliation.BlockUnknown))
				lose(h, path.Join(removal, "blocks", "charlie", "state.json"))
			},
			steps: []string{"bootwright destroy"}, next: "destroy",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			row.arrange(t, h)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil {
				t.Fatal(err)
			}
			next, previewed := "", reconciliation.Apply
			if status.Lifecycle != nil {
				next, previewed = status.Lifecycle.Next, reconciliation.Verb(status.Lifecycle.Verb)
			}
			if status.NextSteps == nil || !slices.Equal(status.NextSteps, row.steps) || next != row.next {
				t.Fatalf("status offers %q and names next %q, want %q and %q", status.NextSteps, next, row.steps, row.next)
			}
			requirePreviewDecidesIfOffered(t, h, previewed, row.steps, row.next)
			for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
				requireDecidesIfOffered(t, h, verb, slices.Contains(row.steps, "bootwright "+string(verb)))
			}
		})
	}
}

// failedChainedRemoval removes charlie and then fails removing bravo, which
// records the removal failed with alpha still pending.
func failedChainedRemoval(t *testing.T, h *harness) {
	t.Helper()
	applyChained(t, h, nil)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a removal whose block failed reported success")
	}
	h.capability.outcomes = nil
}

// requireDecidesIfOffered runs verb under a declined confirmation. Offered, it
// decides: it presents one plan and asks once. Left out, it refuses
// lifecycle.state before it presents or asks. Either way it writes nothing.
func requireDecidesIfOffered(t *testing.T, h *harness, verb reconciliation.Verb, offered bool) {
	t.Helper()
	ctx := context.Background()
	before, mutations := untouchedOf(h), h.workspace.mutations
	presented, asked := len(h.presenter.presented), h.confirmer.asked
	h.confirmer.decline = true
	defer func() { h.confirmer.decline = false }()
	var err error
	if verb == reconciliation.Apply {
		_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
	} else {
		_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	}
	decided := len(h.presenter.presented) == presented+1 && h.confirmer.asked == asked+1
	switch {
	case offered && (err == nil || err.Error() != "declined" || !decided):
		t.Fatalf("the offered %s did not decide: %v (presented %d, asked %d)", verb, err, len(h.presenter.presented)-presented, h.confirmer.asked-asked)
	case !offered && (firstCode(err) != "lifecycle.state" || len(h.presenter.presented) != presented || h.confirmer.asked != asked):
		t.Fatalf("the %s status leaves out = %v, want a lifecycle.state refusal before it presents", verb, err)
	}
	before.require(t, h)
	if h.workspace.mutations != mutations {
		t.Fatalf("the %s took the exclusive lock %d times", verb, h.workspace.mutations-mutations)
	}
}

// requirePreviewDecidesIfOffered holds a plan preview of verb to status: it
// decides where status offers verb, naming the next action status names, and
// refuses lifecycle.state where status leaves it out.
func requirePreviewDecidesIfOffered(t *testing.T, h *harness, verb reconciliation.Verb, steps []string, next string) {
	t.Helper()
	before := untouchedOf(h)
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if slices.Contains(steps, "bootwright "+string(verb)) {
		if err != nil || preview.Verb != string(verb) || preview.Receipt.Next != next {
			t.Fatalf("the preview of the offered %s = %+v (%v), want one naming %q", verb, preview, err, next)
		}
	} else if firstCode(err) != "lifecycle.state" {
		t.Fatalf("the preview of the %s status leaves out = %v, want a lifecycle.state refusal", verb, err)
	}
	before.require(t, h)
}

// Status and plan name what an incomplete operation calls for by one rule: a
// failed removal holding a block not done, an unknown one included, is
// replaced by a fresh removal; in any other operation a block that reads
// unknown is resolved before anything else, whatever state the operation
// records; and an operation whose blocks are all done is finalized by its own
// verb rather than resolved or replaced.
func TestStatusAndPlanNameTheSameNextAction(t *testing.T) {
	ctx := context.Background()
	unknownApply := func(t *testing.T, state reconciliation.OperationState) *harness {
		h := newHarness(t, "alpha")
		applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}})
		rewriteState(t, h, path.Join(currentOperation(t, h), "operation.json"), string(state))
		return h
	}
	for name, test := range map[string]struct {
		arrange func(*testing.T) *harness
		next    string
	}{
		"an unknown apply holding an unknown block": {
			arrange: func(t *testing.T) *harness { return unknownApply(t, reconciliation.OperationUnknown) }, next: "resolve",
		},
		"a running apply holding an unknown block": {
			arrange: func(t *testing.T) *harness { return unknownApply(t, reconciliation.OperationRunning) }, next: "resolve",
		},
		"an unknown apply whose blocks are all done":   {arrange: unknownApplyWithEveryBlockDone, next: "continue-apply"},
		"an unknown destroy whose blocks are all done": {arrange: unknownDestroyWithEveryBlockDone, next: "continue-destroy"},
		"a failed destroy whose blocks are all done": {
			arrange: func(t *testing.T) *harness {
				h, _ := failedRemovalWithEveryBlockDone(t)
				return h
			},
			next: "continue-destroy",
		},
		"a failed destroy holding an unknown block": {
			arrange: func(t *testing.T) *harness {
				h, _ := failedRemovalResolvedAs(t, reconciliation.EffectUnknown, reconciliation.BlockUnknown)
				return h
			},
			next: "destroy",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := test.arrange(t)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || status.Lifecycle == nil {
				t.Fatalf("status = %+v (%v)", status, err)
			}
			preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
			if err != nil {
				t.Fatal(err)
			}
			if status.Lifecycle.Next != test.next || preview.Receipt.Next != test.next {
				t.Fatalf("status names %q and plan %q, want %q", status.Lifecycle.Next, preview.Receipt.Next, test.next)
			}
		})
	}
}
