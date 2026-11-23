package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

type provingCapability struct {
	*testCapability
	refusal error
	proved  []string
}

func (c *provingCapability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if _, bound := material["artifact-server-tls"]; !bound {
		return failure("lifecycle.state", "the prover was not given the material the apply bound", "")
	}
	c.proved = append(c.proved, contextName+"/"+block.ID)
	return c.refusal
}

func provingHarness(t *testing.T, refusal error) (*harness, *provingCapability) {
	t.Helper()
	h := newHarness(t, "artifact-server-lab")
	prover := &provingCapability{testCapability: h.capability, refusal: refusal}
	h.service.capabilities = testResolver{capability: prover}
	return h, prover
}

func TestAFreshApplyRefusesMaterialItsCapabilityRefusesBeforeRegistration(t *testing.T) {
	t.Parallel()
	refusal := secrets.Refusal("part", "the serving certificate is not valid at this time", testContextName, "artifact-server-tls", "renew it, then apply again")
	h, prover := provingHarness(t, refusal)
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if reported, want := diagnostics.Of(err), diagnostics.Of(refusal); !reflect.DeepEqual(reported, want) {
		t.Fatalf("apply refused %+v, want %+v", reported, want)
	}
	if !slices.Equal(prover.proved, []string{"lab/artifact-server-lab"}) {
		t.Fatalf("proved = %v", prover.proved)
	}
	if len(h.workspace.area.files) != 0 || len(h.capability.applies) != 0 || len(h.workspace.reservations) != 0 {
		t.Fatalf("a refused binding registered %d files, applied %v and reserved %v", len(h.workspace.area.files), h.capability.applies, h.workspace.reservations)
	}
	if !slices.Equal(h.binder.bound, []string{"artifact-server-tls"}) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("bound %v and released %v, want the fresh binding released", h.binder.bound, h.binder.released)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("a refused binding left evidence %q", h.workspace.evidence)
	}
}

func TestAContinuationIsNotProvedAgainBeforeRegistration(t *testing.T) {
	t.Parallel()
	h, prover := provingHarness(t, nil)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil || result.Receipt.Next != "continue-apply" {
		t.Fatalf("the seeded failed apply = %+v (%v)", result, err)
	}
	if len(prover.proved) != 1 {
		t.Fatalf("a fresh apply proved its binding %d times", len(prover.proved))
	}
	if result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the continuation = %+v (%v)", result, err)
	}
	if result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the destroy = %+v (%v)", result, err)
	}
	if len(prover.proved) != 1 {
		t.Fatalf("a continuation or a destroy proved a binding again: %v", prover.proved)
	}
}

type applyCheckingCapability struct {
	*testCapability
	check func(context.Context, Probe) error
}

func (c *applyCheckingCapability) CheckApplyStart(ctx context.Context, p Probe) error {
	return c.check(ctx, p)
}

type applyPromptingCapability struct {
	*applyCheckingCapability
	events *[]string
}

func (c *applyPromptingCapability) PromptApplyStart(_ context.Context, p Probe) error {
	*c.events = append(*c.events, "prompt "+p.Block.ID)
	return nil
}

func (*applyPromptingCapability) ApplyStartCheck(block reconciliation.Block) string {
	return "prove access for " + block.ID
}

func TestApplyStartCollectsEveryPromptBeforeItsDescribedChecks(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("tools", reconciliation.StageController),
		definition("artifact-server-lab"),
	})
	progress := &testProgress{}
	h.service.options.Progress = progress
	var events []string
	refusal := failure("machine.enrollment", "remote sudo refused", "")
	checking := &applyCheckingCapability{h.capability, func(_ context.Context, p Probe) error {
		events = append(events, "check "+p.Block.ID)
		if p.Block.ID == "artifact-server-lab" {
			return refusal
		}
		return nil
	}}
	h.service.capabilities = testResolver{capability: &applyPromptingCapability{checking, &events}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if result == nil || result.Receipt.State != "paused" || firstCode(err) != firstCode(refusal) || len(h.capability.calls) != 0 {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, h.capability.calls)
	}
	if want := []string{"prompt tools", "prompt artifact-server-lab", "check tools", "check artifact-server-lab"}; !slices.Equal(events, want) {
		t.Fatalf("start order = %v, want %v", events, want)
	}
	want := []string{
		"check:prove access for tools::running", "check:prove access for tools::ok",
		"check:prove access for artifact-server-lab::running", "check:prove access for artifact-server-lab::failed",
	}
	if !slices.Equal(progress.rows, want) || progress.location != "" {
		t.Fatalf("start check rows = %v at %q, want %v before any log location", progress.rows, progress.location, want)
	}
}

func TestApplyStartRefusalPausesBeforeTheFirstAttempt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "artifact-server-lab")
	progress := &testProgress{}
	h.service.options.Progress = progress
	refusal := failure("machine.enrollment", "remote sudo refused", "")
	proofs := 0
	h.service.capabilities = testResolver{capability: &applyCheckingCapability{h.capability, func(_ context.Context, p Probe) error {
		proofs++
		if p.Context != "lab" || p.Block.ID != "artifact-server-lab" || !reconciliation.ValidOperationID(p.ApplyOperation) {
			t.Fatalf("start proof received %+v", p)
		}
		if _, bound := p.Material["artifact-server-tls"]; !bound || h.confirmer.asked != 1 ||
			len(progress.rows) != 0 || progress.location != "" || len(h.capability.calls) != 0 {
			t.Fatal("the check must run after confirmation and before progress or effects")
		}
		return refusal
	}}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab"})
	if result == nil || result.Receipt.State != "paused" || !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(refusal)) || proofs != 1 {
		t.Fatalf("result=%+v err=%v checks=%d", result, err, proofs)
	}
	if len(result.Blocks) != 1 || result.Blocks[0].Attempts != 0 || result.Blocks[0].State != "pending" || len(h.capability.calls) != 0 {
		t.Fatalf("a refused start check left an attempt or native effect: %+v", result.Blocks)
	}
	gone, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || gone == nil || gone.Receipt.State != "done" || len(h.capability.calls) != 0 {
		t.Fatalf("removing a paused plan with no attempted effects = %+v (%v), calls %v", gone, err, h.capability.calls)
	}
}

func TestApplyStartSkipsPreviewDeclinedApplyAndUnselectedPendingBlocks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("tools", reconciliation.StageController),
		definition("artifact-server-lab"),
	})
	var checked []string
	h.service.capabilities = testResolver{capability: &applyCheckingCapability{h.capability, func(_ context.Context, p Probe) error {
		checked = append(checked, p.Block.ID)
		return nil
	}}}
	if _, err := h.service.Plan(ctx, PlanRequest{ContextName: "lab", Verb: reconciliation.Apply}); err != nil || len(checked) != 0 {
		t.Fatalf("preview checked %v (%v)", checked, diagnostics.Of(err))
	}
	h.confirmer.decline = true
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab"}); err == nil || len(checked) != 0 || h.confirmer.asked != 1 {
		t.Fatalf("declined apply checked %v after %d confirmations (%v)", checked, h.confirmer.asked, err)
	}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", Stages: []string{"controller"}, SkipConfirmation: true}); err != nil || !slices.Equal(checked, []string{"tools"}) || h.confirmer.asked != 1 {
		t.Fatalf("controller apply checked %v after %d confirmations (%v)", checked, h.confirmer.asked, diagnostics.Of(err))
	}
}

func TestApplyStartReturnsCancellationWhenThePlanPauses(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "artifact-server-lab")
	h.service.capabilities = testResolver{capability: &applyCheckingCapability{h.capability, func(context.Context, Probe) error {
		return context.Canceled
	}}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if !errors.Is(err, context.Canceled) || result == nil || result.Receipt.State != "paused" || len(h.capability.calls) != 0 {
		t.Fatalf("canceled apply start = %+v (%v), calls = %v", result, err, h.capability.calls)
	}
}

func TestContinuedApplyChecksUnprovedBlockBeforeStartingResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	refusal := failure("machine.enrollment", "remote sudo refused", "")
	var checks []string
	var checkErr error
	h.service.capabilities = testResolver{capability: &applyCheckingCapability{h.capability, func(_ context.Context, p Probe) error {
		checks = append(checks, p.ApplyOperation)
		return checkErr
	}}}
	request := ApplyRequest{ContextName: "lab", SkipConfirmation: true}
	result, err := h.service.Apply(ctx, request)
	if err == nil || result == nil || result.Receipt.State != "unknown" {
		t.Fatalf("seeded apply = %+v (%v)", result, err)
	}
	before := slices.Clone(h.capability.calls)
	checkErr = refusal
	next, err := h.service.Apply(ctx, request)
	if next == nil || next.Receipt.Operation != result.Receipt.Operation || next.Receipt.State != "unknown" || firstCode(err) != firstCode(refusal) {
		t.Fatalf("continuation = %+v (%v)", next, err)
	}
	if !slices.Equal(checks, []string{result.Receipt.Operation, result.Receipt.Operation}) || !slices.Equal(before, h.capability.calls) {
		t.Fatalf("checks = %v, calls = %v after %v", checks, h.capability.calls, before)
	}
}

func TestApplyStartRefusalPreservesAnInterruptedBlockRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const block = "artifact-server-lab"
	h := newHarness(t, block)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	request := ApplyRequest{ContextName: "lab", SkipConfirmation: true}
	result, err := h.service.Apply(ctx, request)
	if err == nil || result == nil || result.Receipt.State != "unknown" {
		t.Fatalf("seeded apply = %+v (%v)", result, err)
	}
	leaveExecutorDead(t, h, block)
	h.service.capabilities = testResolver{capability: &applyCheckingCapability{h.capability, func(context.Context, Probe) error {
		return failure("machine.enrollment", "remote sudo refused", "")
	}}}
	next, err := h.service.Apply(ctx, request)
	record, states := durableOperation(t, h)
	if next == nil || firstCode(err) != "machine.enrollment" || next.Receipt.State != "running" ||
		record.State != reconciliation.OperationRunning || states[block] != reconciliation.BlockRunning {
		t.Fatalf("continuation = %+v (%v), record = %s, blocks = %v", next, err, record.State, states)
	}
}
