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

func TestProvidedHostnameGatesMachineConsumersAndSharesTheHostExclusion(t *testing.T) {
	machineRef := func(object, family string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.Machine), Object: object, Family: family}
	}
	definitions := []reconciliation.BlockDefinition{
		{ID: "claim", Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{ID: "hostname", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedHostnameFamily, Requires: []reconciliation.ObjectRef{machineRef("witness", machine.HardwareFamily)}},
		{ID: "firewall", Kind: string(api.Machine), Object: "witness", Family: machine.FirewallFamily},
		{ID: "ceph", Stage: reconciliation.StageClusters, Requires: []reconciliation.ObjectRef{machineRef("witness", machine.HardwareFamily), machineRef("witness", machine.FirewallFamily)}},
		{ID: "clients", Stage: reconciliation.StageController, Requires: []reconciliation.ObjectRef{machineRef("witness", machine.HardwareFamily)}},
		{ID: "other-claim", Kind: string(api.Machine), Object: "other", Family: machine.HardwareFamily},
		{ID: "other-ceph", Stage: reconciliation.StageClusters, Requires: []reconciliation.ObjectRef{machineRef("other", machine.HardwareFamily)}},
	}
	got, err := reconciliation.ResolveRequirements(dependOnProvidedUpdates(dependOnEnrollment(definitions)))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{nil, {"claim"}, nil, {"claim", "firewall", "hostname"}, {"claim"}, nil, {"other-claim"}}
	for index, definition := range got {
		if !slices.Equal(definition.Dependencies, want[index]) {
			t.Errorf("%s dependencies = %v, want %v", definition.ID, definition.Dependencies, want[index])
		}
	}
	if !slices.Equal(got[1].Exclusive, []string{"provided-os/witness"}) || !slices.Equal(got[1].Exclusive, got[2].Exclusive) {
		t.Fatal("the hostname and firewall must not run together")
	}
	if len(got[3].Exclusive) != 0 || len(got[4].Exclusive) != 0 || len(got[6].Exclusive) != 0 {
		t.Fatal("a consumer must not take the host exclusion")
	}
}

func TestProvidedHostnameAndUpdateOfOneMachineNeverOverlap(t *testing.T) {
	machineRef := reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily}
	definitions := []reconciliation.BlockDefinition{
		{ID: "claim", Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{ID: "hostname", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedHostnameFamily, Requires: []reconciliation.ObjectRef{machineRef}},
		{ID: "update", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedUpdateFamily, Requires: []reconciliation.ObjectRef{machineRef}},
		{ID: "ceph", Stage: reconciliation.StageClusters, Requires: []reconciliation.ObjectRef{machineRef}},
	}
	got, err := reconciliation.ResolveRequirements(dependOnProvidedUpdates(dependOnEnrollment(definitions)))
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range got[1:3] {
		if !slices.Equal(definition.Exclusive, []string{"provided-os/witness"}) {
			t.Errorf("%s exclusive = %v", definition.ID, definition.Exclusive)
		}
	}
	if want := []string{"claim", "hostname", "update"}; !slices.Equal(got[3].Dependencies, want) {
		t.Errorf("ceph dependencies = %v, want %v", got[3].Dependencies, want)
	}
}

func TestProvidedRegistrationPrecedesTheUpdateAndEveryMachineConsumer(t *testing.T) {
	machineRef := func(family string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: family}
	}
	definitions := []reconciliation.BlockDefinition{
		{ID: "claim", Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{ID: "enroll", Kind: string(api.Machine), Object: "witness", Family: machine.EnrollmentFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "registration", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedRegistrationFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "update", Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedUpdateFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "firewall", Kind: string(api.Machine), Object: "witness", Family: machine.FirewallFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "ceph", Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily), machineRef(machine.FirewallFamily)}},
		{ID: "other", Kind: string(api.Machine), Object: "other", Family: machine.HardwareFamily},
		{ID: "other-ceph", Requires: []reconciliation.ObjectRef{{Kind: string(api.Machine), Object: "other", Family: machine.HardwareFamily}}},
	}
	got, err := reconciliation.ResolveRequirements(dependOnProvidedUpdates(dependOnEnrollment(definitions)))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{nil, {"claim"}, {"claim", "enroll"}, {"claim", "enroll", "registration"}, {"claim", "enroll"}, {"claim", "enroll", "firewall", "registration", "update"}, nil, {"other"}}
	for index, definition := range got {
		if !slices.Equal(definition.Dependencies, want[index]) {
			t.Errorf("%s dependencies = %v, want %v", definition.ID, definition.Dependencies, want[index])
		}
	}
}

func TestProvidedRegistrationFollowsTheHostnameAndPrecedesTheUpdate(t *testing.T) {
	machineRef := func(family string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.Machine), Object: "witness", Family: family}
	}
	definitions := []reconciliation.BlockDefinition{
		{ID: "claim", Kind: string(api.Machine), Object: "witness", Family: machine.HardwareFamily},
		{ID: "hostname", Stage: reconciliation.StageMachines, Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedHostnameFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "registration", Stage: reconciliation.StageMachines, Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedRegistrationFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "update", Stage: reconciliation.StageMachines, Kind: string(api.Machine), Object: "witness", Family: machine.ProvidedUpdateFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "ceph", Stage: reconciliation.StageClusters, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "firewall", Stage: reconciliation.StageMachines, Kind: string(api.Machine), Object: "witness", Family: machine.FirewallFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
		{ID: "enroll", Stage: reconciliation.StageMachines, Kind: string(api.Machine), Object: "witness", Family: machine.EnrollmentFamily, Requires: []reconciliation.ObjectRef{machineRef(machine.HardwareFamily)}},
	}
	got, err := reconciliation.ResolveRequirements(dependOnProvidedUpdates(dependOnEnrollment(definitions)))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{nil, {"claim", "enroll"}, {"claim", "enroll", "hostname"}, {"claim", "enroll", "registration"}, {"claim", "enroll", "hostname", "registration", "update"}, {"claim", "enroll"}, {"claim"}}
	for index, definition := range got {
		if !slices.Equal(definition.Dependencies, want[index]) {
			t.Errorf("%s dependencies = %v, want %v", definition.ID, definition.Dependencies, want[index])
		}
	}
	if len(got[2].Exclusive) != 0 {
		t.Errorf("the registration took the host exclusion %v", got[2].Exclusive)
	}
}
