package clusteraccess

import (
	"strings"
	"testing"
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
