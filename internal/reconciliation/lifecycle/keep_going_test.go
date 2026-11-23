package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type refusingRemoval struct {
	*testCapability
	outcome reconciliation.Outcome
	failure error
}

func (r *refusingRemoval) Destroy(ctx context.Context, execution Execution) (Result, error) {
	result, err := r.testCapability.Destroy(ctx, execution)
	if execution.Block.ID == "worker-a" && r.failure != nil {
		return Result{Outcome: r.outcome}, r.failure
	}
	return result, err
}

var targetLocalRemoval = diagnostics.Stop{
	Code: "test.removal-local", Cause: diagnostics.CauseTarget, Certainty: diagnostics.CertaintyNone, LocalRemovalFailure: true,
}

func TestDestroyKeepGoingContinuesAnIndependentRemovalAndRetriesWhatRemains(t *testing.T) {
	t.Parallel()
	h := scheduled(t, 1, definition("provider"), dependent("worker-a", "provider"), dependent("worker-b", "provider"))
	capability := &refusingRemoval{
		testCapability: h.capability, outcome: reconciliation.OutcomeFailed,
		failure: targetLocalRemoval.Fail("the provider refused this guest's removal", "restore the guest's native removal"),
	}
	h.service.capabilities = testResolver{capability: capability}
	h.capability.secrets = []string{"lab-bmc-credentials"}
	completeApply(t, h)

	request := DestroyRequest{ContextName: testContextName, KeepGoing: true, SkipConfirmation: true}
	result, err := h.service.Destroy(t.Context(), request)
	if err == nil || result == nil || result.Receipt.State != "failed" {
		t.Fatalf("incomplete removal = %+v, %v", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"worker-a", "worker-b"}) {
		t.Fatalf("removals = %v", h.capability.destroys)
	}
	assertRemainingRemoval(t, result.Blocks)
	if len(h.binder.released) != 0 {
		t.Fatalf("partial destroy released %v", h.binder.released)
	}
	if !strings.HasSuffix(result.NextCommand, " --keep-going") {
		t.Fatalf("next command = %q", result.NextCommand)
	}
	status, err := h.service.Status(t.Context(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	assertRemainingRemoval(t, status.Lifecycle.Blocks)

	before := untouchedOf(h)
	preview, err := h.service.Plan(t.Context(), PlanRequest{
		ContextName: testContextName, Verb: reconciliation.Destroy, Readiness: true, KeepGoing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	before.require(t, h)
	if !preview.KeepGoing || len(preview.Steps) != 2 ||
		preview.Steps[0].ID != "worker-a" || preview.Steps[0].PreviousFailure == nil ||
		preview.Steps[0].PreviousFailure.Code != targetLocalRemoval.Code ||
		preview.Steps[1].ID != "provider" || !slices.Equal(preview.Steps[1].After, []int{1}) {
		t.Fatalf("retry preview = %+v", preview)
	}

	// A subsequent inverse can prove the operator already removed the guest.
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnchanged}}
	capability.failure = nil
	result, err = h.service.Destroy(t.Context(), request)
	if err != nil || result == nil || result.Receipt.State != "done" || result.Blocks[0].Outcome != "unchanged" ||
		!slices.Equal(h.capability.destroys, []string{"worker-a", "worker-b", "worker-a", "provider"}) {
		t.Fatalf("retry = %+v, %v, removals = %v", result, err, h.capability.destroys)
	}
	if !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("completed destroy released %v", h.binder.released)
	}
}

func assertRemainingRemoval(t *testing.T, blocks []BlockResult) {
	t.Helper()
	if len(blocks) != 3 || blocks[0].ID != "worker-a" || blocks[0].Failure == nil ||
		blocks[0].Failure.Code != targetLocalRemoval.Code || blocks[1].ID != "worker-b" || blocks[1].State != "done" ||
		blocks[2].ID != "provider" || !slices.Equal(blocks[2].WaitsFor, []string{"worker-a"}) {
		t.Fatalf("remaining cleanup = %+v", blocks)
	}
}

func TestDestroyKeepGoingStopsOnUnprovedOrUnclassifiedFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		keep    bool
		outcome reconciliation.Outcome
		failure error
	}{
		{"default", false, reconciliation.OutcomeFailed, targetLocalRemoval.Fail("target refused", "")},
		{"unclassified", true, reconciliation.OutcomeFailed, errors.New("the shared runtime failed")},
		{"mixed-causes", true, reconciliation.OutcomeFailed, errors.Join(targetLocalRemoval.Fail("target refused", ""), errors.New("the store failed"))},
		{"unproved", true, reconciliation.OutcomeUnknown, targetLocalRemoval.Fail("target refused", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			h := scheduled(t, 1, definition("provider"), dependent("worker-a", "provider"), dependent("worker-b", "provider"))
			h.service.capabilities = testResolver{capability: &refusingRemoval{testCapability: h.capability, outcome: test.outcome, failure: test.failure}}
			completeApply(t, h)
			_, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, KeepGoing: test.keep, SkipConfirmation: true})
			if err == nil || !slices.Equal(h.capability.destroys, []string{"worker-a"}) {
				t.Fatalf("err = %v, removals = %v", err, h.capability.destroys)
			}
		})
	}
}

func TestKeepGoingPreviewAndDestroyRespectAGlobalRefusal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "alpha")
	completeApply(t, h)
	anotherHost(t, h)
	before := untouchedOf(h)
	preview, err := h.service.Plan(t.Context(), PlanRequest{
		ContextName: testContextName, Verb: reconciliation.Destroy, Readiness: true, KeepGoing: true,
	})
	if firstCode(err) != "engine.controller-other-host" || preview == nil ||
		!slices.ContainsFunc(preview.Checks, func(check PlanCheck) bool {
			return check.ID == "destroy-admission" && check.Status == "failed"
		}) {
		t.Fatalf("preview = %+v, err = %v", preview, err)
	}
	_, err = h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, KeepGoing: true, SkipConfirmation: true})
	if firstCode(err) != "engine.controller-other-host" {
		t.Fatalf("destroy = %v", err)
	}
	before.require(t, h)
}

type retirementVerifier struct{ *retiringTestCapability }

func (*retirementVerifier) RetiresHost(terminal, dependent reconciliation.Block, machine string) bool {
	return terminal.ID == "verify" && dependent.ID == "storage" && machine == "storage-node"
}

func TestPendingRetirementUsesItsActualWitness(t *testing.T) {
	t.Parallel()
	plan, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{
		definition("storage"), dependent("verify", "storage"), onMachine(definition("other"), "storage-node"),
	})
	if err != nil {
		t.Fatal(err)
	}
	capability := &retirementVerifier{&retiringTestCapability{
		onlineTestCapability: &onlineTestCapability{testCapability: &testCapability{}},
		hosts:                map[string][]string{"storage": {"storage-node"}},
	}}
	service := Service{capabilities: testResolver{capability: capability}, keepGoing: true}
	phases := service.computePhases(plan)
	states := map[string]reconciliation.BlockState{
		"storage": reconciliation.BlockDone, "other": reconciliation.BlockDone, "verify": reconciliation.BlockFailed,
	}
	if pending := pendingRetirements(plan, phases, states); !slices.Equal(pending, []string{"storage"}) {
		t.Fatalf("pending retirement = %v, phases = %+v", pending, phases)
	}
	states["other"], states["verify"] = reconciliation.BlockPending, reconciliation.BlockDone
	if pending := pendingRetirements(plan, phases, states); len(pending) != 0 {
		t.Fatalf("retirement waits for unrelated work: %v", pending)
	}
}
