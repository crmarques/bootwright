package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func machineWithSSH(name, addr string) v1alpha1.Machine {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: name}}
	m.Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: addr}}
	m.Spec.Access.SSH = &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}
	return m
}

func TestValidateUniqueMachineSSHAddresses(t *testing.T) {
	dup := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithSSH("a", "10.0.0.5"),
		machineWithSSH("b", "10.0.0.5"),
		machineWithSSH("c", "10.0.0.6"),
	}}
	errs := validateUniqueMachineSSHAddresses(dup)
	if len(errs) != 1 || !strings.Contains(errs[0], "a") || !strings.Contains(errs[0], "b") {
		t.Fatalf("duplicate SSH address must refuse and name both machines, got %v", errs)
	}
	ok := v1alpha1.State{Machines: []v1alpha1.Machine{machineWithSSH("a", "10.0.0.5"), machineWithSSH("b", "10.0.0.6")}}
	if e := validateUniqueMachineSSHAddresses(ok); len(e) != 0 {
		t.Fatalf("distinct SSH addresses must pass, got %v", e)
	}
}
