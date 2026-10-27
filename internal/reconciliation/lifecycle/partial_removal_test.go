package lifecycle

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A destroy paused at the online-cleanup boundary has taken back the online
// block of an object whose other block waits behind the Machines, so nothing
// proves what the removal did to that object as a whole: its realization, which
// status reports, reads unknown and diff reads its real side unknown, naming
// the blocks removed and those not yet run, while an object whose every block
// is gone reads pending and absent.
func TestADestroyPausedAtTheOnlineBoundaryReadsAPartlyRemovedObjectUnknown(t *testing.T) {
	definitions := []reconciliation.BlockDefinition{
		stagedDefinition("host", reconciliation.StageSubstrates),
		stagedDefinition("machine", reconciliation.StageMachines, "host"),
		stagedDefinition("installation", reconciliation.StageClusters, "machine"),
		stagedDefinition("storage", reconciliation.StageClusters, "installation"),
	}
	definitions[0].Object = "installation"
	h := newPlannedHarness(t, definitions)
	h.service.capabilities = testResolver{capability: &onlineTestCapability{testCapability: h.capability, online: map[string]bool{"storage": true}}}
	compiler := sourcesCompiler{states: map[string]*compilation.State{}, errs: map[string]error{}}
	compiler.states[digestOf(h.workspace.inputs)] = diffState(servers("v1", "machine", "installation", "storage")...)
	h.service.compiler = compiler
	h.capability.outcomeFor = map[string]Result{"storage": changedWithKubeconfig()}
	completeApply(t, h)
	h.capability.quiescence = map[string]Quiescence{
		"machine": {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --context lab --name machine"},
	}
	if result, err := destroy(h); firstCode(err) != "lifecycle.live" || result == nil || result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("online boundary = %+v (%v)", result, err)
	}
	ctx := context.Background()
	diff, err := h.service.Diff(ctx, DiffRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	type object struct {
		Name, Realization, Real, Reason string
	}
	report := []object{}
	for _, compared := range diff.Objects {
		entry := object{Name: compared.Name, Realization: string(compared.Realization), Real: compared.Real.State, Reason: compared.Real.Reason}
		report = append(report, entry)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "lifecycle-partial-removal", withoutIdentities(data), false)
}
