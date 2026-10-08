package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// unknownApply leaves the harness's apply unknown on its one block alpha.
func unknownApply(t *testing.T, h *harness) {
	t.Helper()
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose block lost its outcome reported success")
	}
	h.capability.outcomeFor, h.capability.calls = nil, nil
}

// A fresh destroy over an unknown apply first asks the apply's observation
// (D123), and a block that observation does not prove completed is resolved
// by the removal's own check (D119), asked next. An absent target is the apply's positive absence of effect; a target that
// still shows everything the removal takes back, or part of it, as this
// context's own is a positive partial realization. Either is recorded against
// the apply's block as failed, never done, and the removal then registers and
// takes the block back.
func TestARemovalResolvesAnUnknownApplyBlockByTheRemovalsCheck(t *testing.T) {
	for _, tc := range []struct {
		observed reconciliation.EffectState
		recorded reconciliation.EffectState
	}{
		{reconciliation.EffectCompleted, reconciliation.EffectNoEffect},
		{reconciliation.EffectNoEffect, reconciliation.EffectPartial},
		{reconciliation.EffectPartial, reconciliation.EffectPartial},
	} {
		t.Run(string(tc.observed), func(t *testing.T) {
			h := newHarness(t, "alpha")
			unknownApply(t, h)
			h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}, {Effect: tc.observed}}
			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			if err != nil || result.Receipt.Verb != string(reconciliation.Destroy) || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
			}
			if len(h.capability.calls) < 2 || !slices.Equal(h.capability.calls[:2], []string{"observe:alpha", "observe-removal:alpha"}) {
				t.Fatalf("the removal's resolution asked %v, want the apply's observation and then the removal's", h.capability.calls)
			}
			store := operationstore.New(h.workspace.area, time.Now)
			resolution, found, err := store.LastResolution(context.Background(), firstOperation, "alpha", 1)
			if err != nil || !found || resolution.Effect != tc.recorded || resolution.Outcome != reconciliation.OutcomeFailed {
				t.Fatalf("the resolution recorded %+v (%t, %v), want %s", resolution, found, err, tc.recorded)
			}
			if record, err := store.Block(context.Background(), firstOperation, "alpha"); err != nil || record.State != reconciliation.BlockFailed {
				t.Fatalf("the apply's block reads %+v (%v), want failed", record, err)
			}
			if !slices.Equal(h.capability.destroys, []string{"alpha"}) {
				t.Fatalf("the removal took back %v", h.capability.destroys)
			}
		})
	}
}

// A target neither the apply's observation proves completed nor the removal's
// own check can read as this context's own leaves the apply's block unknown,
// and the removal refuses, registering nothing.
func TestARemovalLeavesAnUnreadableApplyBlockUnknownAndRegistersNothing(t *testing.T) {
	h := newHarness(t, "alpha")
	unknownApply(t, h)
	applied := currentOperation(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}, {Effect: reconciliation.EffectUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.unknown" {
		t.Fatalf("the removal = %+v (%v), want lifecycle.unknown", diagnostics.Of(err), err)
	}
	if !slices.Equal(h.capability.calls, []string{"observe:alpha", "observe-removal:alpha"}) || len(h.capability.destroys) != 0 {
		t.Fatalf("the refused removal asked %v and took back %v", h.capability.calls, h.capability.destroys)
	}
	if now := currentOperation(t, h); now != applied {
		t.Fatalf("the refused removal registered %q over %q", now, applied)
	}
	if _, states := durableOperation(t, h); states["alpha"] != reconciliation.BlockUnknown {
		t.Fatalf("the apply's block reads %s, want unknown", states["alpha"])
	}
}

// A block the removal's check proves this context's own or absent is the
// removal's to take back, so a removal refused over another block it could not
// prove reports only that block, never the apply's remedy for the proved one.
func TestARefusedRemovalReportsOnlyTheBlocksItsCheckCouldNotProve(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeChanged}, "bravo": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose block lost its outcome reported success")
	}
	h.capability.outcomeFor = nil
	leaveRunning(t, h, "alpha")
	h.service.options.Concurrency = 1
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}, {Effect: reconciliation.EffectCompleted}, {Effect: reconciliation.EffectUnknown}, {Effect: reconciliation.EffectUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 2 || reported[0].Message != "the outcome of bravo is still unknown: "+unexplained.Reason ||
		reported[1].Message != "this removal cannot prove what these effects left behind, so it registered nothing: bravo" {
		t.Fatalf("the refused removal reported %+v (%v)", reported, err)
	}
	if _, states := durableOperation(t, h); states["alpha"] != reconciliation.BlockFailed || states["bravo"] != reconciliation.BlockUnknown {
		t.Fatalf("the apply's blocks read %v", states)
	}
}

// removalReader explains an unproved observation only by the removal's
// reading of it, as a capability whose removal check refuses evidence its
// apply check reads as nothing it can name does.
type removalReader struct{ *testCapability }

func (removalReader) Unresolved(verb reconciliation.Verb, block reconciliation.Block, _ json.RawMessage) (Unresolved, bool) {
	if verb != reconciliation.Destroy {
		return Unresolved{}, false
	}
	return Unresolved{Reason: "the removal cannot read " + block.ID + " as this context's own", Remedy: "restore " + block.ID}, true
}

// A removal that leaves an apply's block unknown explains it by its own
// reading, and status, which reads the apply's block for the apply first,
// falls back to that same reading, so both name one reason and remedy.
func TestAnApplyBlockTheRemovalLeftUnknownIsExplainedByTheRemovalsReading(t *testing.T) {
	h := newHarness(t, "alpha")
	h.service.capabilities = testResolver{capability: removalReader{h.capability}}
	unknownApply(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}, {Effect: reconciliation.EffectUnknown, Evidence: json.RawMessage(`{"foreign":true}`)}}
	want := Unresolved{Reason: "the removal cannot read alpha as this context's own", Remedy: "restore alpha"}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Message != "the outcome of alpha is still unknown: "+want.Reason {
		t.Fatalf("the removal reported %+v (%v)", reported, err)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle == nil || len(status.Lifecycle.Blocks) != 1 || status.Lifecycle.Blocks[0].Unresolved == nil ||
		*status.Lifecycle.Blocks[0].Unresolved != want {
		t.Fatalf("status = %+v (%v), want the block explained as %+v", status.Lifecycle, err, want)
	}
}

// A fresh destroy over an unknown apply whose own observation proves the
// block completed resolves it done (D123), never asking the removal's check,
// and the removal then takes the block back as any completed apply's.
func TestARemovalResolvesAnApplyBlockItsApplyObservationProvesDone(t *testing.T) {
	h := newHarness(t, "alpha")
	unknownApply(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.Verb != string(reconciliation.Destroy) || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if slices.Contains(h.capability.calls, "observe-removal:alpha") || len(h.capability.calls) == 0 || h.capability.calls[0] != "observe:alpha" {
		t.Fatalf("the removal's resolution asked %v, want only the apply's observation", h.capability.calls)
	}
	store := operationstore.New(h.workspace.area, time.Now)
	if record, err := store.Block(context.Background(), firstOperation, "alpha"); err != nil || record.State != reconciliation.BlockDone {
		t.Fatalf("the apply's block reads %+v (%v), want done", record, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"alpha"}) {
		t.Fatalf("the removal took back %v", h.capability.destroys)
	}
}

// A continuation of an unknown apply still resolves its block by the apply's
// own check.
func TestAContinuationOfAnUnknownApplyStillResolvesByTheApplysCheck(t *testing.T) {
	h := newHarness(t, "alpha")
	unknownApply(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.calls, []string{"observe:alpha"}) {
		t.Fatalf("the continuation's resolution asked %v, want only the apply's observation", h.capability.calls)
	}
}
