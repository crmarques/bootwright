package lifecycle_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

// A power command of a KubeVirt Machine reads its pin from the evidence of the
// apply that completed its machine block, which carries the frozen request the
// pin names the VirtualMachine by.
func TestAKubeVirtMachinesPowerPinIsReadFromItsApplysEvidence(t *testing.T) {
	ctx := context.Background()
	journey, host := kubevirtClusterJourney(t)
	holdKubevirtSecrets(journey)
	if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", diagnostics.Of(err))
	}
	for _, name := range kubevirtNodes {
		published, err := journey.Service.Evidence(ctx, lifecycle.JourneyContext, kubevirt.MachineKind, name)
		if err != nil {
			t.Fatalf("the evidence of %s: %v", name, diagnostics.Of(err))
		}
		pin, found, err := kubevirt.PinnedIdentity(name, published)
		want := machineref.HardwareIdentity{UUID: kubevirtHostUID, Serial: host.machines[name]}
		if err != nil || !found || pin != want {
			t.Fatalf("the pin of %s = %+v, %t (%v), want %+v", name, pin, found, diagnostics.Of(err), want)
		}
	}
}
