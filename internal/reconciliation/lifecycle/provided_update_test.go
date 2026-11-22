package lifecycle

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestProvidedUpdateWaitsForEnrollmentAndGatesMachineConsumers(t *testing.T) {
	machineRef := func(family string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: family}
	}
	definitions := []reconciliation.BlockDefinition{
		{ID: "claim", Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{ID: "enroll", Kind: string(api.Machine), Object: "witness", Family: machine.EnrollmentFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "update", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedUpdateFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "firewall", Kind: string(api.Machine), Object: "witness", Family: machine.FirewallFamily},
		{ID: "ceph", Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily), machineRef(machine.FirewallFamily)}},
	}
	got, err := reconciliation.ResolveRequirements(dependOnProvidedUpdates(dependOnEnrollment(definitions)))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{nil, {"claim"}, {"claim", "enroll"}, {"enroll"}, {"claim", "enroll", "firewall", "update"}}
	for index, definition := range got {
		if !slices.Equal(definition.Dependencies, want[index]) {
			t.Errorf("%s dependencies = %v, want %v", definition.ID, definition.Dependencies, want[index])
		}
	}
	if !slices.Equal(got[2].Exclusive, []string{"provided-os/witness"}) || !slices.Equal(got[2].Exclusive, got[3].Exclusive) {
		t.Fatal("the update and firewall must not run together")
	}
}
