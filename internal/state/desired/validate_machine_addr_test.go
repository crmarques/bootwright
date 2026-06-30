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

func TestValidateMachineVirtualMediaCertificate(t *testing.T) {
	machine := func(vm *v1alpha1.BMCVirtualMedia) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: "node"},
			Spec: v1alpha1.MachineSpec{
				Hardware: v1alpha1.MachineHardware{
					Management: v1alpha1.MachineHardwareManagement{
						BMC: v1alpha1.BMCSpec{
							Address:        "redfish-virtualmedia+https://bmc.test/redfish/v1/Systems/1",
							CredentialsRef: v1alpha1.SecretRef{Name: "bmc-creds"},
							VirtualMedia:   vm,
						},
					},
				},
			},
		}
	}
	vmTLS := func(t *v1alpha1.BMCVirtualMediaTLS) *v1alpha1.BMCVirtualMedia {
		return &v1alpha1.BMCVirtualMedia{TLS: t}
	}
	prefix := "Machine/node spec.hardware"

	ok := machine(vmTLS(&v1alpha1.BMCVirtualMediaTLS{ImportServerCertificate: true, RemoveServerCertificateAfterBoot: true}))
	if errs := validateMachineHardware(prefix, ok, v1alpha1.InfraProvider{}, false); containsSubstring(errs, "virtualMedia.tls") {
		t.Fatalf("valid import+remove rejected: %v", errs)
	}

	badRemove := machine(vmTLS(&v1alpha1.BMCVirtualMediaTLS{RemoveServerCertificateAfterBoot: true}))
	if !containsSubstring(validateMachineHardware(prefix, badRemove, v1alpha1.InfraProvider{}, false), "removeServerCertificateAfterBoot requires importServerCertificate") {
		t.Fatalf("expected removeServerCertificateAfterBoot dependency error")
	}

	empty := machine(vmTLS(&v1alpha1.BMCVirtualMediaTLS{}))
	if !containsSubstring(validateMachineHardware(prefix, empty, v1alpha1.InfraProvider{}, false), "sets no option") {
		t.Fatalf("expected empty virtualMedia.tls error")
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
	if !containsSubstring(validateMachineInterfaceAddresses("if", badRef, badRef.Spec.Network.Config), `addressRef "missing" does not resolve`) {
		t.Fatalf("expected addressRef resolution error")
	}

	badPrefix := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 40},
	}, addrs)
	if !containsSubstring(validateMachineInterfaceAddresses("if", badPrefix, badPrefix.Spec.Network.Config), "out of IPv4 range") {
		t.Fatalf("expected IPv4 prefix range error")
	}
}
