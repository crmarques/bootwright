package physicalerase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	receipt := eraseFixtureReceipt(request)
	execution := lifecycle.Execution{Operation: "remove-1", ApplyOperation: "apply-1", Attempt: 2, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Request: canonical}, RequestDigest: receipt.Request}, Material: fleetMaterial()}
	originalInstall, _ := originalPhysicalErase(t)
	originalInstall.State = reconciliation.BlockDone
	execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{originalInstall}
	execution.PrepareRemoval = func(context.Context, json.RawMessage) error { return nil }
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt})
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: proof}}
	capability := New(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: receipt.UUID}})
	execution.RemovalPreparations = []json.RawMessage{mustMarshal(receipt)}
	if _, err := capability.Destroy(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	deadline := runner.requests[0].Deadline
	if deadline <= time.Duration(request.DeadlineSeconds)*time.Second || deadline > lifecycle.MaxDeadline {
		t.Fatalf("the run states %s for a frozen %ds, within %s", deadline, request.DeadlineSeconds, lifecycle.MaxDeadline)
	}
}

func mustMarshal(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
