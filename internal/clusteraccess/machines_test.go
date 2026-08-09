package clusteraccess

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestResolveMachinesSelectsClusterNode(t *testing.T) {
	state := loadFixtureState(t, "003-3nodes-libvirt")
	sel, err := ResolveMachines(state, "master-0")
	if err != nil {
		t.Fatalf("ResolveMachines(master-0): %v", err)
	}
	if !sel.MachineSelection || !sel.Active {
		t.Fatalf("expected active machine selection, got %+v", sel)
	}
	if !sel.MachineProvision["master-0"] {
		t.Fatalf("MachineProvision should contain master-0, got %v", sel.MachineProvision)
	}
	if !sel.MachineHosts["bastion"] {
		t.Fatalf("MachineHosts closure should include libvirt provider host bastion, got %v", sel.MachineHosts)
	}
	found := false
	for _, r := range sel.ContainerRoots {
		if r == "3-nodes-ocp-libvirt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ContainerRoots should include owning cluster, got %v", sel.ContainerRoots)
	}
}

func TestResolveMachinesUnknownMachine(t *testing.T) {
	state := loadFixtureState(t, "003-3nodes-libvirt")
	if _, err := ResolveMachines(state, "ghost"); err == nil || !strings.Contains(err.Error(), "unknown machine(s): ghost") {
		t.Fatalf("ResolveMachines(ghost) err = %v, want unknown-machine error", err)
	}
}

func TestResolveMachinesRefusesStandaloneMachineWithExternalRemedy(t *testing.T) {
	state := v1alpha1.State{Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "standalone"}}}}
	_, err := ResolveMachines(state, "standalone")
	if err == nil {
		t.Fatal("standalone machine must fail closed because Bootwright planned no provisioning work for it")
	}
	for _, want := range []string{"no provisioning work", "no bootwright retry command", "restore the intended cluster, shared-service, or provider reference", "decommission the machine out of band"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("standalone refusal missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "`bootwright ") {
		t.Fatalf("standalone refusal must not invent an executable remedy: %v", err)
	}
}

func TestResolveMachinesProviderHostIsProvisionable(t *testing.T) {
	state := loadFixtureState(t, "003-3nodes-libvirt")
	sel, err := ResolveMachines(state, "bastion")
	if err != nil {
		t.Fatalf("ResolveMachines(bastion) should succeed (provider/service host): %v", err)
	}
	if !sel.MachineProvision["bastion"] {
		t.Fatalf("MachineProvision should contain bastion, got %v", sel.MachineProvision)
	}
}
