package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func machineWithBMC(name, address string) v1alpha1.Machine {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: name}}
	m.Spec.Hardware.Management.BMC.Address = address
	return m
}

func TestValidateUniqueBMCAddresses(t *testing.T) {
	dup := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithBMC("node-a", "https://10.0.0.5"),
		machineWithBMC("node-b", "https://10.0.0.5"),
		machineWithBMC("node-c", "https://10.0.0.6"),
		machineWithBMC("vm-1", ""),
	}}
	errs := validateUniqueBMCAddresses(dup)
	if len(errs) != 1 {
		t.Fatalf("expected one duplicate-BMC finding, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "node-a") || !strings.Contains(errs[0], "node-b") {
		t.Fatalf("finding must name both colliding machines: %q", errs[0])
	}
	if strings.Contains(errs[0], "node-c") {
		t.Fatalf("finding must not implicate the unique-address machine: %q", errs[0])
	}

	ok := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithBMC("node-a", "https://10.0.0.5"),
		machineWithBMC("node-b", "https://10.0.0.6"),
		machineWithBMC("vm-1", ""),
	}}
	if errs := validateUniqueBMCAddresses(ok); len(errs) != 0 {
		t.Fatalf("distinct/empty BMC addresses must pass, got: %v", errs)
	}
}

func TestValidateUniqueBMCAddressesNormalizesEquivalentSpellings(t *testing.T) {
	dup := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithBMC("node-a", "redfish+https://bmc-1/redfish/v1/Systems/1"),
		machineWithBMC("node-b", "redfish-virtualmedia+https://bmc-1/redfish/v1/Systems/1"),
	}}
	if errs := validateUniqueBMCAddresses(dup); len(errs) != 1 {
		t.Fatalf("equivalent redfish spellings of one BMC must collide, got %d: %v", len(errs), errs)
	}

	scheme := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithBMC("node-a", "redfish://bmc-2/"),
		machineWithBMC("node-b", "https://bmc-2"),
	}}
	if errs := validateUniqueBMCAddresses(scheme); len(errs) != 1 {
		t.Fatalf("redfish:// and https:// of one BMC must collide, got %d: %v", len(errs), errs)
	}

	distinct := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithBMC("node-a", "redfish+https://bmc-3/redfish/v1/Systems/1"),
		machineWithBMC("node-b", "redfish+https://bmc-3/redfish/v1/Systems/2"),
	}}
	if errs := validateUniqueBMCAddresses(distinct); len(errs) != 0 {
		t.Fatalf("distinct System IDs must pass, got: %v", errs)
	}
}
