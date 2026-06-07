package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func interfaceAddrMachine(ifAddr []v1alpha1.MachineInterfaceAddress, addrs []v1alpha1.MachineAddress) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec: v1alpha1.MachineSpec{
			Addresses: addrs,
			Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
				NetworkConfigRef:   v1alpha1.LocalObjectReference{Name: "net"},
				InterfaceAddresses: ifAddr,
			}},
		},
	}
}

func TestValidateMachineInterfaceAddresses(t *testing.T) {
	addrs := []v1alpha1.MachineAddress{{Name: "ip", Address: "192.0.2.20"}}

	ok := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 24},
	}, addrs)
	if errs := validateMachineInterfaceAddresses("if", ok, ok.Spec.Network.Config); len(errs) != 0 {
		t.Fatalf("valid interfaceAddresses rejected: %v", errs)
	}

	badRef := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "missing"}, PrefixLength: 24},
	}, addrs)
	if !containsSubstring(validateMachineInterfaceAddresses("if", badRef, badRef.Spec.Network.Config), `addressRef.name "missing" does not resolve`) {
		t.Fatalf("expected addressRef resolution error")
	}

	badPrefix := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 40},
	}, addrs)
	if !containsSubstring(validateMachineInterfaceAddresses("if", badPrefix, badPrefix.Spec.Network.Config), "out of IPv4 range") {
		t.Fatalf("expected IPv4 prefix range error")
	}
}
