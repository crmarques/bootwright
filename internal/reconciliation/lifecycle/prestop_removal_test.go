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

const preStopMachineStop = "bootwright machine stop --context lab --name machine"

func preStopHarness(t *testing.T, unrelated bool) (*harness, *onlineTestCapability) {
	t.Helper()
	h, c := onlineHarness(t, unrelated)
	c.online, c.preStop = map[string]bool{}, map[string]bool{"installation": true}
	return h, c
}

func occurrences(values []string, value string) int {
	return len(slices.DeleteFunc(slices.Clone(values), func(candidate string) bool { return candidate != value }))
}

func TestPreStopRemovalRunsBeforeTheStopProof(t *testing.T) {
	h, capability := preStopHarness(t, false)
	completeApply(t, h)
	h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "its domain is running", Stop: preStopMachineStop}}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || slices.Contains(preview.Stops, "machine") || !slices.Equal(preview.StopsAfterOnline, []string{"machine"}) {
		t.Fatalf("pre-stop preview = %+v (%v)", preview, err)
	}
	capability.onProbe = func(probe Probe) {
		if probe.Block.ID == "machine" && !slices.Contains(h.capability.destroys, "installation") {
			t.Errorf("machine was probed before the pre-stop inverse: %v", h.capability.destroys)
		}
	}
	result, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || result == nil || result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("pre-stop boundary = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"installation"}) || blockState(t, h, "storage") != reconciliation.BlockPending || blockState(t, h, "machine") != reconciliation.BlockPending {
		t.Fatalf("before stop removed %v", h.capability.destroys)
	}
	if !strings.Contains(diagnostics.Of(err)[0].Remediation, preStopMachineStop) {
		t.Fatalf("boundary lost its stop remedy: %v", err)
	}
	paused := result.Receipt.Operation
	preview, err = h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || !preview.Continuation || !slices.Equal(preview.StopsAfterOnline, []string{"machine"}) {
		t.Fatalf("paused preview = %+v (%v)", preview, err)
	}
	h.capability.quiescence["machine"] = Quiescence{State: Quiescent}
	result, err = destroy(h)
	if err != nil || result.Receipt.Operation != paused || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("after operator stop = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"installation", "storage", "machine", "host"}) {
		t.Fatalf("continued removal = %v", h.capability.destroys)
	}
}

func TestPreStopRemovalKeepsHostBoundRetirement(t *testing.T) {
	h, capability := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	capability.preStop = map[string]bool{"installation": true}
	completeApply(t, h)
	applied := len(h.capability.executions)
	h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --context lab --name rhel-01"}}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || !slices.Equal(preview.StopsAfterOnline, []string{"rhel-01"}) {
		t.Fatalf("retiring pre-stop preview = %+v (%v)", preview, err)
	}
	result, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || result.Receipt.State != string(reconciliation.OperationPaused) || strings.Contains(diagnostics.Of(err)[0].Message, "its hosts outlive this removal") {
		t.Fatalf("pre-stop retirement boundary = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"installation"}) || !retiredProbes(capability)["storage"] {
		t.Fatalf("before stop removed %v; retired %v", h.capability.destroys, retiredProbes(capability))
	}
	h.capability.quiescence["machine"] = Quiescence{State: Quiescent}
	result, err = destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("after operator stop = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"installation", "storage", "machine", "host"}) {
		t.Fatalf("retired removal = %v", h.capability.destroys)
	}
	for _, execution := range h.capability.executions[applied:] {
		if execution.Block.ID == "storage" && !execution.RetiredWithHosts {
			t.Fatalf("storage removal lost its retirement: %+v", execution)
		}
	}
}

func TestPreStopRemovalCrashOnBothSidesOfTheStopBoundary(t *testing.T) {
	t.Run("before its durable completion", func(t *testing.T) {
		h, _ := preStopHarness(t, false)
		completeApply(t, h)
		h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "running", Stop: preStopMachineStop}}
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		result, err := destroy(h)
		if err == nil || result.Receipt.State != string(reconciliation.OperationUnknown) || !slices.Equal(h.capability.destroys, []string{"installation"}) || slices.Contains(h.capability.probes, "machine") {
			t.Fatalf("lost pre-stop inverse = %+v, removed %v, probes %v (%v)", result, h.capability.destroys, h.capability.probes, err)
		}
		h.capability.outcomes, h.capability.observations = nil, []Observation{{Effect: reconciliation.EffectCompleted}}
		result, err = destroy(h)
		if firstCode(err) != "lifecycle.live" || result.Receipt.State != string(reconciliation.OperationPaused) || !slices.Contains(h.capability.calls, "observe-removal:installation") || !slices.Equal(h.capability.destroys, []string{"installation"}) {
			t.Fatalf("resolved pre-stop = %+v, removed %v (%v)", result, h.capability.destroys, err)
		}
		h.capability.quiescence["machine"] = Quiescence{State: Quiescent}
		result, err = destroy(h)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || occurrences(h.capability.destroys, "installation") != 1 {
			t.Fatalf("after stop = %+v, removed %v (%v)", result, h.capability.destroys, err)
		}
	})
	t.Run("after its durable completion", func(t *testing.T) {
		h, _ := preStopHarness(t, false)
		completeApply(t, h)
		h.capability.destroyHold = func(id string) {
			if id == "installation" {
				h.capability.quiescentErr = errors.New("probe lost")
			}
		}
		result, err := destroy(h)
		if err == nil || result == nil || blockState(t, h, "installation") != reconciliation.BlockDone || !slices.Equal(h.capability.destroys, []string{"installation"}) {
			t.Fatalf("seeded boundary failure = %+v, removed %v (%v)", result, h.capability.destroys, err)
		}
		h.capability.destroyHold, h.capability.quiescentErr = nil, nil
		result, err = destroy(h)
		if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || !slices.Equal(h.capability.destroys, []string{"installation", "storage", "machine", "host"}) {
			t.Fatalf("after boundary = %+v, removed %v (%v)", result, h.capability.destroys, err)
		}
	})
}

func TestPreStopRemovalKeepsUnrelatedMachinesStopFirst(t *testing.T) {
	h, _ := preStopHarness(t, true)
	completeApply(t, h)
	before, _ := durableOperation(t, h)
	h.capability.quiescence = map[string]Quiescence{"unrelated": {State: Live, Reason: "in use"}}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || !slices.Contains(preview.Stops, "unrelated") || slices.Contains(preview.Stops, "machine") || !slices.Equal(preview.StopsAfterOnline, []string{"machine"}) {
		t.Fatalf("unrelated preview = %+v (%v)", preview, err)
	}
	_, err = destroy(h)
	after, _ := durableOperation(t, h)
	if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 || after.ID != before.ID {
		t.Fatalf("live unrelated Machine admitted removal: %v (%v)", h.capability.destroys, err)
	}
}
