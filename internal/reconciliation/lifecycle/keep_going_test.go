package lifecycle

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
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

func TestKeepGoingRetainsUnobservedEnrollmentAndRemovesIndependentEffects(t *testing.T) {
	t.Parallel()
	h := scheduled(t, 1, definition("provider"), dependent("enrollment", "provider"), definition("artifact"))
	h.capability.secrets = []string{"lab-bmc-credentials"}
	h.capability.outcomeFor = map[string]Result{"enrollment": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("the interrupted enrollment completed")
	}
	source := currentOperation(t, h)
	request := DestroyRequest{ContextName: testContextName, KeepGoing: true, SkipConfirmation: true}
	h.capability.observeErr = diagnostics.NewFailure("test.unknown", "ownership could not be read", "")
	if _, err := h.service.Destroy(t.Context(), request); !slices.ContainsFunc(diagnostics.Of(err), func(d diagnostics.Diagnostic) bool {
		return d.Code == "destroy.unproved-effects"
	}) || len(h.capability.destroys) != 0 {
		t.Fatalf("unclassified source error = %v, effects = %v", diagnostics.Of(err), h.capability.destroys)
	}
	h.capability.observeErr = machine.EnrollmentSudoTTYRefused("node", "operator")
	partial, err := h.service.Destroy(t.Context(), request)
	if err == nil || partial == nil || partial.Receipt.State != "paused" || !slices.Equal(h.capability.destroys, []string{"artifact"}) {
		t.Fatalf("partial destroy = %+v, %v, effects = %v", partial, err, h.capability.destroys)
	}
	if len(h.binder.released) != 0 || len(partial.Blocks) != 3 ||
		partial.Blocks[1].ID != "enrollment" || partial.Blocks[1].Unresolved == nil ||
		partial.Blocks[2].ID != "provider" || !slices.Equal(partial.Blocks[2].WaitsFor, []string{"enrollment"}) {
		t.Fatalf("partial blocks = %+v, released = %v", partial.Blocks, h.binder.released)
	}
	if !slices.Contains(h.capability.probes, "provider") {
		t.Fatalf("deferred provider skipped mandatory quiescence: %v", h.capability.probes)
	}
	status, err := h.service.Status(t.Context(), StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle.Blocks[1].Unresolved == nil {
		t.Fatalf("retained enrollment status = %+v, %v", status, err)
	}
	before := untouchedOf(h)
	preview, err := h.service.Plan(t.Context(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, KeepGoing: true, Readiness: true})
	if err != nil || preview.Steps[1].Selection != StepResolve {
		t.Fatalf("retained enrollment preview = %+v, %v", preview, err)
	}
	before.require(t, h)
	if _, err := destroy(h); err == nil || !slices.Equal(h.capability.destroys, []string{"artifact"}) {
		t.Fatalf("plain destroy passed the unresolved proof: %v, effects = %v", err, h.capability.destroys)
	}
	rewriteState(t, h, path.Join(partial.Receipt.Operation, "operation.json"), string(reconciliation.OperationRunning))
	h.workspace.evidence, err = projection(reconciliation.Destroy, reconciliation.OperationRunning)
	if err != nil {
		t.Fatal(err)
	}
	sourceRecord := path.Join(source, "blocks", "enrollment", "state.json")
	record := h.workspace.area.files[sourceRecord]
	delete(h.workspace.area.files, sourceRecord)
	if _, err := h.service.Destroy(t.Context(), request); firstCode(err) != "engine.records-damaged" ||
		!slices.Equal(h.capability.destroys, []string{"artifact"}) {
		t.Fatalf("missing original enrollment record admitted work: %v, effects = %v", err, h.capability.destroys)
	}
	h.workspace.area.files[sourceRecord] = record
	h.capability.observeErr = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: []byte(`{"ok":true}`)}}
	finished, err := destroy(h)
	if err != nil || finished == nil || finished.Receipt.Operation != partial.Receipt.Operation || finished.Receipt.State != "done" ||
		!slices.Equal(h.capability.destroys, []string{"artifact", "enrollment", "provider"}) ||
		!slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("resumed destroy = %+v, %v, effects = %v, released = %v", finished, err, h.capability.destroys, h.binder.released)
	}
}

func TestDeferredEnrollmentProtectsItsNativePowerTarget(t *testing.T) {
	t.Parallel()
	machineBlock := onMachine(dependent("machine", "provider"), "node")
	independent := onMachine(definition("independent"), "other")
	plan, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{
		definition("enrollment"), dependent("provider", "enrollment"), machineBlock, independent,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := Service{capabilities: testResolver{bindings: []CapabilityBinding{
		{Kind: machineBlock.Kind, Implementation: machineBlock.Implementation, Family: reconciliation.ManagedMachineFamily},
	}}}
	skip := heldRemovalBlocks(plan, map[string]error{"enrollment": machine.EnrollmentSudoTTYRefused("node", "operator")})
	group := service.deferMachineStops(plan, skip)
	if group == nil || len(group.machines) != 1 || group.machines[0].ID != independent.ID || !skip["machine"] {
		t.Fatalf("group = %+v, excluded = %v", group, skip)
	}
}
