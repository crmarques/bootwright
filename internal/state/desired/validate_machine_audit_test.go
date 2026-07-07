package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestInterfaceAddressResolvesTemplateInterface covers the phantom-interface guard:
// interfaceAddresses[].interface must name an interface the effective NetworkConfig
// template declares, else the inject silently mints a typeless phantom carrying the
// install IP.
func TestInterfaceAddressResolvesTemplateInterface(t *testing.T) {
	config := v1alpha1.MachineNetworkConfig{
		NetworkConfigRef:   v1alpha1.LocalObjectReference{Name: "net"},
		InterfaceAddresses: []v1alpha1.MachineInterfaceAddress{{Interface: "vlan10", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 24}},
	}
	template := map[string]bool{"vlan10": true, "bond0": true}
	if errs := validateInterfaceAddressesResolveInterface("if", config, template); len(errs) != 0 {
		t.Fatalf("interface present in template rejected: %v", errs)
	}
	typo := config
	typo.InterfaceAddresses = []v1alpha1.MachineInterfaceAddress{{Interface: "vlan01", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 24}}
	if !containsSubstring(validateInterfaceAddressesResolveInterface("if", typo, template), `interface "vlan01" does not match any interface in the effective NetworkConfig`) {
		t.Fatalf("expected phantom-interface rejection")
	}
	if errs := validateInterfaceAddressesResolveInterface("if", typo, nil); len(errs) != 0 {
		t.Fatalf("nil template must disable the check, got %v", errs)
	}
}

// TestInterfaceAddressFamilyMatchesLiteral covers the family/literal guard: a v6
// literal with the family omitted (defaults to ipv4) is rejected; matching family is
// accepted.
func TestInterfaceAddressFamilyMatchesLiteral(t *testing.T) {
	v6 := []v1alpha1.MachineAddress{{Name: "ip", Address: "fd00:132::20"}}

	slip := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 64},
	}, v6)
	if !containsSubstring(validateMachineInterfaceAddresses("if", slip, slip.Spec.Network.Config), "is not an ipv4 address") {
		t.Fatalf("expected v6 literal without family to be rejected")
	}

	tagged := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", Family: "ipv6", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 64},
	}, v6)
	if errs := validateMachineInterfaceAddresses("if", tagged, tagged.Spec.Network.Config); len(errs) != 0 {
		t.Fatalf("v6 literal with family ipv6 rejected: %v", errs)
	}

	v4WrongFamily := interfaceAddrMachine([]v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", Family: "ipv6", AddressRef: v1alpha1.LocalObjectReference{Name: "ip4"}, PrefixLength: 64},
	}, []v1alpha1.MachineAddress{{Name: "ip4", Address: "192.0.2.20"}})
	if !containsSubstring(validateMachineInterfaceAddresses("if", v4WrongFamily, v4WrongFamily.Spec.Network.Config), "is not an ipv6 address") {
		t.Fatalf("expected v4 literal with family ipv6 to be rejected")
	}
}

func machineWithInterfaceAddress(name, addrName, ip string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Addresses: []v1alpha1.MachineAddress{{Name: addrName, Address: ip}},
			Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
				NetworkConfigRef:   v1alpha1.LocalObjectReference{Name: "net"},
				InterfaceAddresses: []v1alpha1.MachineInterfaceAddress{{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: addrName}, PrefixLength: 24}},
			}},
		},
	}
}

func TestValidateUniqueMachineInstallIPs(t *testing.T) {
	clash := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithInterfaceAddress("worker-1", "ip", "192.168.141.31"),
		machineWithInterfaceAddress("worker-2", "ip", "192.168.141.31"),
	}}
	if !containsSubstring(validateUniqueMachineInstallIPs(clash), `install IP "192.168.141.31" is assigned by interfaceAddresses on more than one Machine (worker-1, worker-2)`) {
		t.Fatalf("expected duplicate install IP rejection")
	}
	distinct := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithInterfaceAddress("worker-1", "ip", "192.168.141.31"),
		machineWithInterfaceAddress("worker-2", "ip", "192.168.141.32"),
	}}
	if errs := validateUniqueMachineInstallIPs(distinct); len(errs) != 0 {
		t.Fatalf("distinct install IPs rejected: %v", errs)
	}
}

func machineWithNICMAC(name, mac string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Hardware: v1alpha1.MachineHardware{NICs: []v1alpha1.MachineNIC{{Name: "primary", MACAddress: mac}}},
		},
	}
}

func TestValidateUniqueMachineNICMACs(t *testing.T) {
	clash := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithNICMAC("worker-1", "52:54:00:41:31:10"),
		machineWithNICMAC("worker-2", "52:54:00:41:31:10"),
	}}
	if !containsSubstring(validateUniqueMachineNICMACs(clash), "is declared by more than one Machine (worker-1, worker-2)") {
		t.Fatalf("expected duplicate NIC MAC rejection")
	}
	// Case-insensitive: an upper-case copy still collides.
	caseClash := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithNICMAC("worker-1", "52:54:00:41:31:10"),
		machineWithNICMAC("worker-2", "52:54:00:41:31:10"),
	}}
	caseClash.Machines[1].Spec.Hardware.NICs[0].MACAddress = "52:54:00:41:31:10"
	if len(validateUniqueMachineNICMACs(caseClash)) == 0 {
		t.Fatalf("expected duplicate NIC MAC rejection (case-insensitive)")
	}
	distinct := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithNICMAC("worker-1", "52:54:00:41:31:10"),
		machineWithNICMAC("worker-2", "52:54:00:41:31:11"),
	}}
	if errs := validateUniqueMachineNICMACs(distinct); len(errs) != 0 {
		t.Fatalf("distinct NIC MACs rejected: %v", errs)
	}
}

func TestMACInVSphereManualRange(t *testing.T) {
	if !macInVSphereManualRange("00:50:56:12:34:56") {
		t.Fatalf("in-range MAC reported out of range")
	}
	if macInVSphereManualRange("00:50:56:40:00:00") {
		t.Fatalf("MAC above 3f reported in range")
	}
	if macInVSphereManualRange("52:54:00:aa:bb:cc") {
		t.Fatalf("hardware MAC reported in range")
	}
}

func TestVSphereAuthoredMACRangeValidation(t *testing.T) {
	provider := v1alpha1.InfraProvider{Metadata: v1alpha1.Metadata{Name: "vc"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerVSphere}}
	machine := func(mac string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: "node"},
			Spec:     v1alpha1.MachineSpec{Hardware: v1alpha1.MachineHardware{NICs: []v1alpha1.MachineNIC{{Name: "primary", MACAddress: mac}}}},
		}
	}
	bad := machine("52:54:00:aa:bb:cc")
	if !containsSubstring(validateMachineHardware("Machine/node spec.hardware", bad, provider, true), "outside the vCenter manually-assigned MAC range") {
		t.Fatalf("expected out-of-range vSphere MAC rejection")
	}
	ok := machine("00:50:56:3a:bb:cc")
	if containsSubstring(validateMachineHardware("Machine/node spec.hardware", ok, provider, true), "vCenter manually-assigned") {
		t.Fatalf("in-range vSphere MAC rejected")
	}
	// A baremetal provider must not apply the vCenter range.
	bm := v1alpha1.InfraProvider{Metadata: v1alpha1.Metadata{Name: "rack"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}}
	if containsSubstring(validateMachineHardware("Machine/node spec.hardware", bad, bm, true), "vCenter manually-assigned") {
		t.Fatalf("baremetal provider incorrectly applied vCenter MAC range")
	}
}

func TestMachineInstallStringListInternalWhitespace(t *testing.T) {
	if !containsSubstring(validateMachineInstallStringList("pkg", []string{"vim tiny"}), `"vim tiny" must not contain internal whitespace`) {
		t.Fatalf("expected internal-whitespace rejection")
	}
	if errs := validateMachineInstallStringList("pkg", []string{"vim", "cephadm"}); len(errs) != 0 {
		t.Fatalf("clean token list rejected: %v", errs)
	}
	// The leading/trailing message still fires unchanged for edge whitespace.
	if !containsSubstring(validateMachineInstallStringList("pkg", []string{" vim"}), "must not contain leading or trailing whitespace") {
		t.Fatalf("expected leading-whitespace rejection")
	}
}

func TestMachineInstallRepositoryIDCharacters(t *testing.T) {
	if !containsSubstring(validateMachineInstallRepositories("repos", []v1alpha1.MachineInstallRepository{{ID: `extra "tools`, BaseURL: "https://repo.test"}}), "must not contain whitespace or quotes") {
		t.Fatalf("expected quoted repo id rejection")
	}
	if errs := validateMachineInstallRepositories("repos", []v1alpha1.MachineInstallRepository{{ID: "extras", BaseURL: "https://repo.test"}}); len(errs) != 0 {
		t.Fatalf("clean repo rejected: %v", errs)
	}
}

func TestMachineInstallOSFloor(t *testing.T) {
	if errs := validateMachineInstallOSFloor("os", v1alpha1.MachineInstallOS{Family: "rhel", Version: "9.7"}); len(errs) != 0 {
		t.Fatalf("rhel 9.7 rejected: %v", errs)
	}
	if errs := validateMachineInstallOSFloor("os", v1alpha1.MachineInstallOS{Family: "rhel", Version: "10.1"}); len(errs) != 0 {
		t.Fatalf("rhel 10.1 rejected: %v", errs)
	}
	if !containsSubstring(validateMachineInstallOSFloor("os", v1alpha1.MachineInstallOS{Family: "rhel", Version: "8.10"}), "below the supported floor") {
		t.Fatalf("expected rhel 8.10 rejection")
	}
	if !containsSubstring(validateMachineInstallOSFloor("os", v1alpha1.MachineInstallOS{Family: "centos", Version: "9"}), "is not supported") {
		t.Fatalf("expected non-rhel family rejection")
	}
	// Empty fields are the caller's non-empty checks, not this one's.
	if errs := validateMachineInstallOSFloor("os", v1alpha1.MachineInstallOS{}); len(errs) != 0 {
		t.Fatalf("empty OS rejected here: %v", errs)
	}
}
