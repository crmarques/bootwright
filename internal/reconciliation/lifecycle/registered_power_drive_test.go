package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

type recordedOperations struct {
	OperationStore
	found map[string]operationstore.Operation
}

func (r recordedOperations) ReadOperation(_ context.Context, id string) (operationstore.Operation, error) {
	if operation, ok := r.found[id]; ok {
		return operation, nil
	}
	return operationstore.Operation{}, errors.New("operation record missing")
}

func provedMachine(operation, object string, state reconciliation.BlockState) BlockEvidence {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "machine-first-v1-" + object, Kind: "Machine", Object: object, Implementation: "machine-first-v1"}}
	return BlockEvidence{Operation: operation, Block: block, Kind: "Machine", Object: object, Implementation: "machine-first-v1", State: state, Evidence: json.RawMessage(`{}`)}
}

// A power transition drives only a Machine whose original apply proved it done,
// through the executable's own drive, and refuses every other request as a
// damaged record rather than guessing a target.
func TestMachinePowerDrivesOnlyAMachineItsApplyProvedDone(t *testing.T) {
	h := newHarness(t)
	h.service.capabilities = claimResolver{claimResolver{}.Bindings()[0]: claimingCapability("machine-first-v1", "node-a")}
	var driven []string
	h.service.options.DriveMachine = func(_ context.Context, _ Execution, proof BlockEvidence, on bool) error {
		driven = append(driven, proof.Object)
		if !on {
			return errors.New("stop refused")
		}
		return nil
	}
	run := invocation{operation: operationstore.Operation{ID: "apply-1", Verb: reconciliation.Apply}}
	execution := Execution{ApplyProved: []BlockEvidence{provedMachine("apply-1", "node-a", reconciliation.BlockDone), provedMachine("apply-1", "node-c", reconciliation.BlockFailed)}}
	power := h.service.machinePower(run, execution)
	if err := power(t.Context(), "node-a", true); err != nil || len(driven) != 1 || driven[0] != "node-a" {
		t.Fatalf("a proved machine drove %v: %v", driven, err)
	}
	if err := power(t.Context(), "node-a", false); err == nil || err.Error() != "stop refused" {
		t.Fatalf("the drive's refusal was lost: %v", err)
	}
	driven = nil
	for _, name := range []string{"node-b", "node-c"} {
		if found := diagnostics.Of(power(t.Context(), name, true)); len(found) != 1 || len(driven) != 0 {
			t.Fatalf("%s was driven %v or did not refuse: %+v", name, driven, found)
		}
	}
	h.service.options.DriveMachine = nil
	power = h.service.machinePower(run, execution)
	if found := diagnostics.Of(power(t.Context(), "node-a", true)); len(found) != 1 {
		t.Fatalf("a build without a drive started a machine: %+v", found)
	}
}

// An additive apply only starts a cluster node its earlier apply installed, and
// starts each such node once through that apply's recorded credential binding,
// refusing a record that holds none.
func TestAnAdditiveApplyStartsAnInstalledMachineOnceThroughItsRecordedBinding(t *testing.T) {
	h := newHarness(t)
	h.service.capabilities = claimResolver{claimResolver{}.Bindings()[0]: claimingCapability("machine-first-v1", "node-a")}
	h.binder.material = map[string]secrets.Material{"bmc": versionedValue("recorded-password")}
	var seen []string
	h.service.options.DriveMachine = func(_ context.Context, execution Execution, _ BlockEvidence, on bool) error {
		value, _ := execution.Material["bmc"].Part(secrets.ValuePart)
		seen = append(seen, string(value))
		if !on {
			t.Fatal("an additive apply stopped a machine")
		}
		return nil
	}
	store := recordedOperations{found: map[string]operationstore.Operation{
		"apply-1": {ID: "apply-1", Bindings: []string{"bind-1"}}, "apply-0": {ID: "apply-0"},
	}}
	run := invocation{operation: operationstore.Operation{ID: "apply-2", Verb: reconciliation.Apply}, store: store, clusterStarts: &sync.Map{}}
	execution := Execution{Context: "lab", ApplyProved: []BlockEvidence{provedMachine("apply-1", "node-a", reconciliation.BlockDone)}}
	power := h.service.machinePower(run, execution)
	for range 2 {
		if err := power(t.Context(), "node-a", true); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 1 || seen[0] != "recorded-password" {
		t.Fatalf("the installed machine was started with %v, want one start with the recorded credential", seen)
	}
	if err := power(t.Context(), "node-a", false); len(diagnostics.Of(err)) != 1 || len(seen) != 1 {
		t.Fatalf("an additive apply stopped an installed machine: %v", err)
	}
	if err := h.service.machinePower(invocation{operation: run.operation, store: store}, execution)(t.Context(), "node-a", true); len(diagnostics.Of(err)) != 1 || len(seen) != 1 {
		t.Fatalf("an additive apply without cluster starts drove a machine: %v", err)
	}

	execution.ApplyProved = []BlockEvidence{provedMachine("apply-0", "node-a", reconciliation.BlockDone)}
	run.clusterStarts = &sync.Map{}
	if found := diagnostics.Of(h.service.machinePower(run, execution)(t.Context(), "node-a", true)); len(found) != 1 || len(seen) != 1 {
		t.Fatalf("an apply without a credential binding was started: %+v", found)
	}
	execution.ApplyProved = []BlockEvidence{provedMachine("apply-9", "node-a", reconciliation.BlockDone)}
	run.clusterStarts = &sync.Map{}
	if err := h.service.machinePower(run, execution)(t.Context(), "node-a", true); err == nil || len(seen) != 1 {
		t.Fatalf("an apply with no record was started: %v", err)
	}
	execution.ApplyProved = []BlockEvidence{provedMachine("apply-1", "node-a", reconciliation.BlockDone)}
	run.clusterStarts = &sync.Map{}
	h.binder.released = []string{"bind-1"}
	if err := h.service.machinePower(run, execution)(t.Context(), "node-a", true); err == nil || len(seen) != 1 {
		t.Fatalf("a released binding was reopened: %v", err)
	}
}
