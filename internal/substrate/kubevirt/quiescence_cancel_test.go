package kubevirt

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type cancellingRunner struct{ cancel context.CancelFunc }

func (r cancellingRunner) Run(ctx context.Context, _ lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.cancel()
	return lifecycle.RunResult{}, ctx.Err()
}

func TestMachineQuiescenceKeepsACancelledProbeACancellation(t *testing.T) {
	block := machineBlock(t)
	probe := lifecycle.Probe{ApplyOperation: "op-1", Context: testContext, Block: block,
		ApplyProved: []lifecycle.BlockEvidence{provedHostEvidence(t, hostBlock(t), "cluster-1")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := NewMachine(cancellingRunner{cancel}).Quiescent(ctx, probe)
	if !errors.Is(err, context.Canceled) || state.State == lifecycle.Unproved {
		t.Fatalf("cancelled probe = %+v (%v), want the cancellation", state, err)
	}
}
