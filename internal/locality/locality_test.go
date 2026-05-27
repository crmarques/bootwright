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
			return "vrt3007", nil
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
		{address: "vrt3007", want: true},
		{address: "vrt3007.bndes.net", want: true},
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
