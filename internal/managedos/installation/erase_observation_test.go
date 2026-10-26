package installation

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A started erase's removal observation is partial only when the adapter
// reads this request's own unfinished custody; evidence that is malformed,
// names another request or version, or claims a removal its receipt does not
// prove stays unknown, so it never admits the destructive retry a partial
// removal does (Q65, D183).
func TestAnUnreadableEraseObservationStaysUnknown(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	receipt := eraseFixtureReceipt(request)
	execution := lifecycle.Execution{Operation: "remove-1", ApplyOperation: "apply-1", Attempt: 1, Material: fleetMaterial(),
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Request: canonical}, RequestDigest: receipt.Request}}
	originalInstall, _ := originalPhysicalErase(t)
	originalInstall.State = reconciliation.BlockDone
	execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{originalInstall}
	encoded := func(evidence EraseEvidence) json.RawMessage {
		data, _ := json.Marshal(evidence)
		return data
	}
	for name, test := range map[string]struct {
		evidence json.RawMessage
		effect   reconciliation.EffectState
	}{
		"its own unfinished custody":      {encoded(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true}), reconciliation.EffectPartial},
		"its custody never prepared":      {encoded(EraseEvidence{Version: EraseImplementation, Request: receipt.Request}), reconciliation.EffectPartial},
		"its complete receipt":            {encoded(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt}), reconciliation.EffectCompleted},
		"malformed evidence":              {json.RawMessage(`{"version":`), reconciliation.EffectUnknown},
		"empty evidence":                  {nil, reconciliation.EffectUnknown},
		"another request's evidence":      {encoded(EraseEvidence{Version: EraseImplementation, Request: "b" + receipt.Request[1:], Prepared: true}), reconciliation.EffectUnknown},
		"another version's evidence":      {encoded(EraseEvidence{Version: "physical-erase-v0", Request: receipt.Request, Prepared: true}), reconciliation.EffectUnknown},
		"a removal its receipt disproves": {encoded(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true}), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: test.evidence}}
			capability := NewErase(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: receipt.UUID}})
			observation, err := capability.ObserveRemoval(context.Background(), execution)
			if err != nil && test.effect != reconciliation.EffectUnknown || observation.Effect != test.effect || len(runner.requests) != 1 {
				t.Fatalf("observation = %s (%v), want %s after one read", observation.Effect, err, test.effect)
			}
		})
	}
}
