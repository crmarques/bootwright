package lifecycle

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The runner records the context, block and description of every run against
// its job, so a held job refuses only its own context's next run and names
// what it is doing. RunFor carries them from the attempt it authorizes, and
// every attempt the engine makes names the context it runs for.
func TestRunForCarriesTheContextBlockAndDescription(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "artifact-server-lab", Description: "serve artifact-server-lab"}}
	request := RunFor(Execution{Context: "lab", Block: block}, Invocation{})
	if request.Context != "lab" || request.Block != "artifact-server-lab" || request.Description != "serve artifact-server-lab" {
		t.Fatalf("the run names context %q, block %q and description %q", request.Context, request.Block, request.Description)
	}
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.executions) == 0 {
		t.Fatal("the apply made no attempt")
	}
	for _, execution := range h.capability.executions {
		if execution.Context != testContextName {
			t.Fatalf("an attempt of %s names context %q", execution.Block.ID, execution.Context)
		}
	}
}

// probeRecorder keeps every probe a removal's quiescence gate makes.
type probeRecorder struct {
	*testCapability
	probes []Probe
}

func (c *probeRecorder) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	c.probes = append(c.probes, probe)
	return c.testCapability.Quiescent(ctx, probe)
}

// A quiescence probe runs an adapter before any operation exists, so it names
// its context as an attempt does, and the execution it presents carries it.
func TestAProbeCarriesItsContext(t *testing.T) {
	if got := (Probe{Context: "lab"}).Execution().Context; got != "lab" {
		t.Fatalf("a probe's execution names context %q", got)
	}
	h := newHarness(t, "artifact-server-lab")
	recorder := &probeRecorder{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: recorder}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.probes) == 0 {
		t.Fatal("the removal probed nothing")
	}
	for _, probe := range recorder.probes {
		if probe.Context != testContextName {
			t.Fatalf("the probe of %s names context %q", probe.Block.ID, probe.Context)
		}
	}
}
