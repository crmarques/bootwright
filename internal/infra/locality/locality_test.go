package locality

import (
	"net"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCheckControllerUsesLocalhost(t *testing.T) {
	result := CheckController(v1alpha1.State{}, Policy{})
	if !result.OK {
		t.Fatalf("CheckController failed: %s", result.Evidence)
	}
	if !strings.Contains(result.Evidence, "localhost") {
		t.Fatalf("evidence = %q, want localhost execution evidence", result.Evidence)
	}
}

func TestIsControllerLocalAddress(t *testing.T) {
	policy := Policy{Deps: Deps{
		Hostname: func() (string, error) {
			return "controller", nil
		},
		InterfaceAddrs: func() ([]net.Addr, error) {
			return []net.Addr{
				&net.IPNet{IP: net.ParseIP("10.7.3.2"), Mask: net.CIDRMask(24, 32)},
			}, nil
		},
		LookupIP: func(host string) ([]net.IP, error) {
			switch host {
			case "bastion-alias.example.test":
				return []net.IP{net.ParseIP("10.7.3.2")}, nil
			case "remote.example.test":
				return []net.IP{net.ParseIP("10.7.3.3")}, nil
			default:
				return nil, &net.DNSError{Err: "not found", Name: host}
			}
		},
	}}

	cases := []struct {
		address string
		want    bool
	}{
		{address: "localhost", want: true},
		{address: "127.0.0.1", want: true},
		{address: "controller", want: true},
		{address: "controller.example.test", want: true},
		{address: "10.7.3.2", want: true},
		{address: "10.7.3.3", want: false},
		{address: "bastion-alias.example.test", want: true},
		{address: "remote.example.test", want: false},
		{address: "bastion.example.test", want: false},
	}
	for _, tc := range cases {
		if got := IsControllerLocalAddress(tc.address, policy); got != tc.want {
			t.Fatalf("IsControllerLocalAddress(%q) = %v, want %v", tc.address, got, tc.want)
		}
	}
}

func TestIsControllerLocalMachineProvidedWithoutSSH(t *testing.T) {
	provided := true
	notProvided := false

	policy := Policy{Deps: Deps{
		Hostname:       func() (string, error) { return "somewhere-else", nil },
		InterfaceAddrs: func() ([]net.Addr, error) { return nil, nil },
		LookupIP:       func(string) ([]net.IP, error) { return nil, nil },
	}}

	local := v1alpha1.Machine{Spec: v1alpha1.MachineSpec{
		OS:     v1alpha1.MachineOSSpec{Provided: &provided},
		Access: v1alpha1.MachineAccess{Local: true},
	}}
	if !IsControllerLocalMachine(local, policy) {
		t.Fatalf("machine declaring spec.access.local should be the controller")
	}

	undeclared := v1alpha1.Machine{Spec: v1alpha1.MachineSpec{
		OS: v1alpha1.MachineOSSpec{Provided: &provided},
	}}
	if IsControllerLocalMachine(undeclared, policy) {
		t.Fatalf("provided-OS machine with no access block must not be assumed local; locality is declared with spec.access.local")
	}

	remote := v1alpha1.Machine{Spec: v1alpha1.MachineSpec{
		OS:        v1alpha1.MachineOSSpec{Provided: &provided},
		Addresses: []v1alpha1.MachineAddress{{Name: "ip", Address: "10.9.9.9"}},
		Access:    v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}}},
	}}
	if IsControllerLocalMachine(remote, policy) {
		t.Fatalf("provided-OS machine with a remote ssh address should not be local")
	}

	node := v1alpha1.Machine{Spec: v1alpha1.MachineSpec{
		OS: v1alpha1.MachineOSSpec{Provided: &notProvided},
	}}
	if IsControllerLocalMachine(node, policy) {
		t.Fatalf("non-provided machine with no ssh block should not be treated as local")
	}
}
