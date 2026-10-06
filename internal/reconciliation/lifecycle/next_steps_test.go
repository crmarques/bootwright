package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
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
			steps: []string{"bootwright context delete --name lab --purge --allow-orphans"},
		},
		{
			name: "a lost index beside pristine evidence",
			arrange: func(t *testing.T, h *harness) {
				completeRemoval(t, h)
				lose(h, "index.json")
			},
			steps: []string{"bootwright context delete --name lab --purge"},
		},
		{
			name: "a lost index beside unreadable evidence",
			arrange: func(t *testing.T, h *harness) {
				completeApply(t, h)
				lose(h, "index.json")
				h.workspace.evidence = []byte("{")
			},
			steps: []string{},
		},
		{
			name: "a completed destroy holding a block not done",
			arrange: func(t *testing.T, h *harness) {
				completeRemoval(t, h)
				lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
			},
			steps: []string{"bootwright context delete --name lab --purge"}, next: "none",
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

// completeRemoval leaves the context holding a completed destroy of what a
// completed apply realized, under pristine evidence.
func completeRemoval(t *testing.T, h *harness) {
	t.Helper()
	completeApply(t, h)
	if result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the removal = %+v (%v)", result, err)
	}
	if !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("the completed removal left evidence %q", h.workspace.evidence)
	}
}

// Where both verbs refuse and name a deletion of the context as the exit,
// status offers exactly the deletion that refusal names: over a lost index,
// over a completed removal holding a block that is not done, and over a lost
// frozen binding.
func TestStatusOffersTheDeletionItsRefusalNames(t *testing.T) {
	ctx := context.Background()
	for name, arrange := range map[string]func(*testing.T) *harness{
		"a lost index beside protected evidence": func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			completeApply(t, h)
			lose(h, "index.json")
			return h
		},
		"a lost index beside pristine evidence": func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			completeRemoval(t, h)
			lose(h, "index.json")
			return h
		},
		"a completed destroy holding a block not done beside pristine evidence": func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			completeRemoval(t, h)
			lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
			return h
		},
		"a completed destroy holding a block not done beside protected evidence": func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			completeApply(t, h)
			h.binder.releaseErr = errors.New("the custody store cannot drop the binding")
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("a removal whose release failed reported success")
			}
			h.binder.releaseErr = nil
			lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
			return h
		},
		"a failed apply whose frozen binding is lost": func(t *testing.T) *harness {
			h := lostBindingStates()["a failed apply"](t)
			deleteBinding(t, h)
			return h
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := arrange(t)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || len(status.NextSteps) == 0 || !strings.HasPrefix(status.NextSteps[0], "bootwright context delete --name lab --purge") {
				t.Fatalf("status = %+v (%v)", status, err)
			}
			_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || !containsCommand(reported[0].Remediation, status.NextSteps[0]) {
				t.Fatalf("status offers %q, the apply refused with %+v (%v)", status.NextSteps[0], reported, err)
			}
		})
	}
}

// containsCommand reports whether text names command whole, with no further
// flag after it.
func containsCommand(text, command string) bool {
	for offset := 0; ; {
		at := strings.Index(text[offset:], command)
		if at < 0 {
			return false
		}
		offset += at + len(command)
		if !strings.HasPrefix(text[offset:], " --") {
			return true
		}
	}
}

// Status offers setup in place of every step that the controller setup gates:
// beside no operation, the first apply claims the host only once its setup
// completed, and over an incomplete operation, a continuation or resolution
// that still has a block to run re-proves that setup first. A finalization
// and a replacement are not held to it and stay offered. Each offered verb
// decides; each verb setup replaces refuses naming setup, before any effect.
func TestStatusOffersSetupWhereSetupGatesTheVerb(t *testing.T) {
	ctx := context.Background()
	failedReceipt := func(h *harness) { h.workspace.controller.State.Receipt.Status = "failed" }
	for _, row := range []struct {
		name    string
		arrange func(*testing.T) *harness
		steps   []string
		gated   []reconciliation.Verb
	}{
		{
			name: "no controller record and no operation",
			arrange: func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				h.workspace.controller.Exists, h.workspace.controller.Initialized = false, false
				return h
			},
			steps: []string{"bootwright setup"}, gated: []reconciliation.Verb{reconciliation.Apply},
		},
		{
			name: "a failed receipt and no operation",
			arrange: func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				failedReceipt(h)
				return h
			},
			steps: []string{"bootwright setup"}, gated: []reconciliation.Verb{reconciliation.Apply},
		},
		{
			name: "a failed receipt over an unknown apply",
			arrange: func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}})
				failedReceipt(h)
				return h
			},
			steps: []string{"bootwright setup", "bootwright destroy"}, gated: []reconciliation.Verb{reconciliation.Apply},
		},
		{
			name: "a failed receipt over a lagging apply",
			arrange: func(t *testing.T) *harness {
				h, _ := laggingApply(t)
				failedReceipt(h)
				return h
			},
			steps: []string{"bootwright apply", "bootwright destroy"},
		},
		{
			name: "a failed receipt over a failed destroy",
			arrange: func(t *testing.T) *harness {
				h := newPlannedHarness(t, chainedDefinitions())
				failedChainedRemoval(t, h)
				failedReceipt(h)
				return h
			},
			steps: []string{"bootwright destroy"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			h := row.arrange(t)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.NextSteps, row.steps) {
				t.Fatalf("status = %+v (%v), want steps %q", status, err, row.steps)
			}
			for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
				offered := slices.Contains(row.steps, "bootwright "+string(verb))
				switch {
				case slices.Contains(row.gated, verb):
					requireSetupRefuses(t, h, verb)
				case offered && verb == reconciliation.Apply && status.Lifecycle != nil && status.Lifecycle.Next == "continue-apply":
					requireFinalizes(t, row.arrange(t))
				case offered || status.Lifecycle != nil:
					requireDecidesIfOffered(t, h, verb, offered)
				}
			}
		})
	}
}

// requireSetupRefuses runs verb confirmed over records whose continuation or
// first claim the controller setup gates. It refuses naming setup before any
// block runs, is probed or observed, and the context still holds the operation
// it held.
func requireSetupRefuses(t *testing.T, h *harness, verb reconciliation.Verb) {
	t.Helper()
	ctx := context.Background()
	held := currentOperation(t, h)
	applies, destroys, observes := len(h.capability.applies), len(h.capability.destroys), len(h.capability.observes)
	var err error
	if verb == reconciliation.Apply {
		_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || (reported[0].Code != "controller.identity" && reported[0].Code != "controller.state") ||
		!strings.Contains(reported[0].Remediation, "bootwright setup") {
		t.Fatalf("the %s status replaces with setup = %+v (%v), want a refusal naming setup", verb, reported, err)
	}
	if len(h.capability.applies) != applies || len(h.capability.destroys) != destroys || len(h.capability.observes) != observes {
		t.Fatalf("the refused %s ran, removed or observed a block", verb)
	}
	if now := currentOperation(t, h); now != held {
		t.Fatalf("the refused %s left the context holding %q, not %q", verb, now, held)
	}
}

// requireFinalizes runs the apply status offers over an apply whose blocks are
// all done: it finalizes the apply without a block, an observation or a
// presented plan, whatever the controller setup reads.
func requireFinalizes(t *testing.T, h *harness) {
	t.Helper()
	h.service.options.Confirmer = nil
	applies, observes, presented := len(h.capability.applies), len(h.capability.observes), len(h.presenter.presented)
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName})
	if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.State != "done" {
		t.Fatalf("the offered apply = %+v (%v)", result, err)
	}
	if len(h.capability.applies) != applies || len(h.capability.observes) != observes || len(h.presenter.presented) != presented {
		t.Fatal("the finalization ran, observed or presented something")
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
		"a failed apply whose blocks are all done": {
			arrange: func(t *testing.T) *harness {
				h, _ := laggingApply(t)
				return h
			},
			next: "continue-apply",
		},
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
