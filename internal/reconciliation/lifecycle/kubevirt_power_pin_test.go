package lifecycle_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
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

// The pin a power command of a KubeVirt Machine is held to is the identity
// its completed machine block recorded: under the apply that recorded it, and
// while a destroy that takes that apply back is current, before and after
// that destroy removed the VirtualMachine.
func TestAKubeVirtMachinesPowerPinIsTheApplysWhileADestroyIsCurrent(t *testing.T) {
	ctx := context.Background()
	journey, host := kubevirtClusterJourney(t)
	holdKubevirtSecrets(journey)
	if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", diagnostics.Of(err))
	}
	recorded := map[string]string{}
	for _, name := range kubevirtNodes {
		recorded[name] = host.machines[name]
	}
	pinned := func(stage string, read func(context.Context, string, string, string) ([]lifecycle.BlockEvidence, error)) {
		t.Helper()
		for _, name := range kubevirtNodes {
			published, err := read(ctx, lifecycle.JourneyContext, kubevirt.MachineKind, name)
			if err != nil {
				t.Fatalf("%s: the evidence of %s: %v", stage, name, diagnostics.Of(err))
			}
			pin, found, err := kubevirt.PinnedIdentity(name, published)
			want := machineref.HardwareIdentity{UUID: kubevirtHostUID, Serial: recorded[name]}
			if err != nil || !found || pin != want {
				t.Fatalf("%s: the pin of %s = %+v, %t (%v), want %+v", stage, name, pin, found, diagnostics.Of(err), want)
			}
		}
	}
	removal := func(state reconciliation.BlockState) {
		t.Helper()
		for _, name := range kubevirtNodes {
			current, err := journey.Service.Evidence(ctx, lifecycle.JourneyContext, kubevirt.MachineKind, name)
			if err != nil || len(current) != 1 || current[0].Verb != reconciliation.Destroy || current[0].State != state {
				t.Fatalf("the removal of %s = %+v (%v), want a destroy %s", name, current, err, state)
			}
		}
	}
	pinned("under the apply", journey.Service.Evidence)
	pinned("the applied evidence under the apply", journey.Service.AppliedEvidence)
	for _, name := range kubevirtNodes {
		host.running[name], host.claims[name] = false, false
	}
	host.failAt = "eject"
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
	holdKubevirtSecrets(journey)
	if _, err := journey.Service.Destroy(ctx, destroy); err == nil || host.failAt != "" {
		t.Fatalf("the interrupted release did not stop the destroy: %v", err)
	}
	removal(reconciliation.BlockPending)
	pinned("while a destroy is current", journey.Service.AppliedEvidence)
	holdKubevirtSecrets(journey)
	if _, err := journey.Service.Destroy(ctx, destroy); err != nil {
		t.Fatalf("destroy: %v", diagnostics.Of(err))
	}
	if len(host.machines) != 0 {
		t.Fatalf("the destroy left %v", host.machines)
	}
	removal(reconciliation.BlockDone)
	pinned("once the destroy removed the VirtualMachines", journey.Service.AppliedEvidence)
}
