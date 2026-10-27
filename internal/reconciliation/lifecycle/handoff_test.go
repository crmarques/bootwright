package lifecycle

import (
	"context"
	"errors"
	"path"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func TestSettledApplyRestoresALatchedLogFault(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	id := completeApply(t, h)
	changeOperation(t, h, id, func(record *operationstore.Operation) { record.LogFault = true })
	logPath := "append " + operationstore.OperationLogPath(id)
	h.workspace.area.fail[logPath] = errors.New("no space")
	before := untouchedOf(h)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName}); firstCode(err) != "runtime.log" {
		t.Fatalf("settle with an unrestored log fault: %v", err)
	}
	before.require(t, h)
	delete(h.workspace.area.fail, logPath)
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName})
	if err != nil || !result.Settled || result.Receipt.Operation != id {
		t.Fatalf("settle = %+v (%v)", result, err)
	}
	if record, states := durableOperation(t, h); record.LogFault || record.ID != id || record.State != reconciliation.OperationDone || states["artifact-server-lab"] != reconciliation.BlockDone {
		t.Fatalf("settled records = %+v %v", record, states)
	}
	if len(h.capability.applies) != 1 || len(h.capability.observes) != 0 || len(h.capability.probes) != 0 || len(h.capability.destroys) != 0 {
		t.Fatal("restoring a settled operation repeated a host effect or probe")
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil || !status.Handoff.Ready || len(status.Handoff.Reasons) != 0 {
		t.Fatalf("restored handoff = %+v (%v)", status, err)
	}
}

func TestSettledLogRestorationReprovesItsRecords(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	id := completeApply(t, h)
	changeOperation(t, h, id, func(record *operationstore.Operation) { record.LogFault = true })
	before := beforeItsTransaction(h, func() {
		changeOperation(t, h, id, func(record *operationstore.Operation) { record.LogFault = false })
	})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName}); firstCode(err) != "lifecycle.state" {
		t.Fatalf("restoration after the record moved = %v", err)
	}
	before.require(t, h)
}

func TestSettledLogRestorationReprovesItsInputInItsTransaction(t *testing.T) {
	for name, change := range map[string]func(f diffFixture){
		"a new revision of the same bytes": func(f diffFixture) { f.update("rev-2", f.workspace.inputs) },
		"edited bytes": func(f diffFixture) {
			f.publish("rev-2", "kind: Environment\n# edited\n", diffState(servers("v1", "alpha", "bravo")...))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiffHarness(t, "alpha")
			id := f.apply(t)
			changeOperation(t, f.harness, id, func(record *operationstore.Operation) { record.LogFault = true })
			before := beforeItsTransaction(f.harness, func() { change(f) })
			applyRefusedWith(t, f, changedMessage, changedRemedy)
			before.require(t, f.harness)
			if record, _ := durableOperation(t, f.harness); !record.LogFault || record.ID != id {
				t.Fatalf("the refused restoration cleared the log fault: %+v", record)
			}
		})
	}
}

func TestSettledLogRestorationRequiresADurableClear(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	id := completeApply(t, h)
	changeOperation(t, h, id, func(record *operationstore.Operation) { record.LogFault = true })
	h.workspace.area.fail["replace "+path.Join(id, "operation.json")] = errors.New("no space")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName}); err == nil {
		t.Fatal("an uncleared log fault reported a successful settle")
	}
	if record, _ := durableOperation(t, h); !record.LogFault || len(h.capability.applies) != 1 || len(h.capability.observes) != 0 {
		t.Fatal("a failed clear lost the fault or repeated an effect")
	}
}

func TestStatusHandoffReadsRecordsWithoutWriting(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*testing.T, *harness)
		reasons []string
	}{
		{"no apply", func(*testing.T, *harness) {}, []string{"no apply is recorded"}},
		{"ready", func(t *testing.T, h *harness) { completeApply(t, h) }, []string{}},
		{"apply not done", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			changeOperation(t, h, id, func(record *operationstore.Operation) { record.State = reconciliation.OperationRunning })
		}, []string{"the apply is running, not done"}},
		{"block not done", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			rewriteState(t, h, path.Join(id, "blocks", "alpha", "state.json"), "failed")
		}, []string{"block alpha is failed, not done"}},
		{"unknown", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			rewriteState(t, h, path.Join(id, "blocks", "alpha", "state.json"), "unknown")
		}, []string{"block alpha is unknown, not done", "block alpha has an unresolved unknown outcome"}},
		{"running", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			rewriteState(t, h, path.Join(id, "blocks", "alpha", "state.json"), "running")
		}, []string{"block alpha is running, not done", "block alpha has an unresolved unknown outcome"}},
		{"log fault", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			changeOperation(t, h, id, func(record *operationstore.Operation) { record.LogFault = true })
		}, []string{"the operation has a latched required-log fault"}},
		{"a new revision of the frozen bytes", func(t *testing.T, h *harness) {
			completeApply(t, h)
			h.update("rev-2", h.workspace.inputs)
		}, []string{"the selected input is not the revision " + testRevision + " the apply froze; bootwright diff --context lab names what changed"}},
		{"edited input", func(t *testing.T, h *harness) {
			completeApply(t, h)
			h.update("rev-2", diffSources("kind: Environment\n# edited\n"))
		}, []string{"the selected input is not the revision " + testRevision + " the apply froze; bootwright diff --context lab names what changed"}},
		{"destroy", func(t *testing.T, h *harness) {
			completeApply(t, h)
			if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
		}, []string{"the latest operation is a destroy"}},
		{"ordered reasons", func(t *testing.T, h *harness) {
			id := completeApply(t, h)
			changeOperation(t, h, id, func(record *operationstore.Operation) {
				record.State, record.LogFault = reconciliation.OperationUnknown, true
			})
			rewriteState(t, h, path.Join(id, "blocks", "alpha", "state.json"), "unknown")
			rewriteState(t, h, path.Join(id, "blocks", "bravo", "state.json"), "pending")
		}, []string{"the apply is unknown, not done", "block alpha is unknown, not done", "block bravo is pending, not done", "block alpha has an unresolved unknown outcome", "the operation has a latched required-log fault"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
			tc.arrange(t, h)
			before, mutations := untouchedOf(h), h.workspace.mutations
			result, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
			if err != nil || result.Handoff.Ready != (len(tc.reasons) == 0) || !slices.Equal(result.Handoff.Reasons, tc.reasons) {
				t.Fatalf("handoff = %+v (%v), want %v", result, err, tc.reasons)
			}
			before.require(t, h)
			if h.workspace.mutations != mutations {
				t.Fatal("status took a mutating transaction")
			}
			if tc.name == "log fault" && !slices.Equal(result.NextSteps, []string{"bootwright apply --context lab"}) {
				t.Fatal("status did not offer the settled apply that restores the log")
			}
		})
	}
}
