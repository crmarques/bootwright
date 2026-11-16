package libvirt

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type cancellingRunner struct{ cancel context.CancelFunc }

func (r cancellingRunner) Run(ctx context.Context, _ lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.cancel()
	return lifecycle.RunResult{}, ctx.Err()
}

func TestMachineQuiescenceKeepsACancelledProbeACancellation(t *testing.T) {
	requests, _ := MachineRequests(labCatalog(), "controller", testContext)
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	probe := lifecycle.Probe{Context: "lab-b", Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical},
		RequestDigest:   "digest",
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := NewMachine(cancellingRunner{cancel}).Quiescent(ctx, probe)
	if !errors.Is(err, context.Canceled) || state.State == lifecycle.Unproved {
		t.Fatalf("cancelled probe = %+v (%v), want the cancellation", state, err)
	}
}
