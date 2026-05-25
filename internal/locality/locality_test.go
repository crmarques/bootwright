package locality

import (
	"errors"
	"net"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCheckBastionMatchesLocalNonLoopbackIP(t *testing.T) {
	state := localityState([]string{"127.0.0.1", "192.0.2.10"})
	result := CheckBastion(state, testPolicy([]string{"192.0.2.10/24"}, nil, "node1"))
	if !result.OK {
		t.Fatalf("CheckBastion failed: %s", result.Evidence)
	}
}

func TestCheckBastionRejectsLoopbackWhenNonLoopbackDeclared(t *testing.T) {
	state := localityState([]string{"127.0.0.1", "192.0.2.10"})
	result := CheckBastion(state, testPolicy([]string{"127.0.0.1/8"}, nil, "node1"))
	if result.OK {
		t.Fatalf("CheckBastion accepted loopback with non-loopback bastion address: %s", result.Evidence)
	}
}

func TestCheckBastionAcceptsLoopbackOnlyAddress(t *testing.T) {
	state := localityState([]string{"127.0.0.1"})
	result := CheckBastion(state, testPolicy([]string{"127.0.0.1/8"}, nil, "localhost"))
	if !result.OK {
		t.Fatalf("CheckBastion rejected loopback-only bastion: %s", result.Evidence)
	}
}

func TestCheckBastionMatchesLocalHostnameResolution(t *testing.T) {
	state := localityState([]string{"bastion.example.test"})
	result := CheckBastion(state, testPolicy(
		[]string{"192.0.2.11/24"},
		map[string][]net.IP{"bastion.example.test": {net.ParseIP("192.0.2.11")}},
		"node1",
	))
	if !result.OK {
		t.Fatalf("CheckBastion failed: %s", result.Evidence)
	}
}

func TestCheckBastionRequiresHostRef(t *testing.T) {
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "lab"}}}}
	result := CheckBastion(state, testPolicy([]string{"192.0.2.10/24"}, nil, "node1"))
	if result.OK || result.Evidence == "" {
		t.Fatalf("CheckBastion should require Environment.spec.bastion.hostRef, got %+v", result)
	}
}

func localityState(addresses []string) v1alpha1.State {
	hostAddresses := make([]v1alpha1.HostAddress, 0, len(addresses))
	for i, address := range addresses {
		hostAddresses = append(hostAddresses, v1alpha1.HostAddress{Name: string(rune('a' + i)), Address: address})
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.EnvironmentSpec{
				Bastion: &v1alpha1.EnvironmentBastionSpec{HostRef: "bastion"},
			},
		}},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec:     v1alpha1.HostSpec{Addresses: hostAddresses},
		}},
	}
}

func testPolicy(interfaceCIDRs []string, lookup map[string][]net.IP, hostname string) Policy {
	return Policy{
		RequireBastion: true,
		Deps: Deps{
			InterfaceAddrs: func() ([]net.Addr, error) {
				out := make([]net.Addr, 0, len(interfaceCIDRs))
				for _, cidr := range interfaceCIDRs {
					ip, network, err := net.ParseCIDR(cidr)
					if err != nil {
						return nil, err
					}
					network.IP = ip
					out = append(out, network)
				}
				return out, nil
			},
			LookupIP: func(name string) ([]net.IP, error) {
				if ips, ok := lookup[name]; ok {
					return ips, nil
				}
				return nil, errors.New("not found")
			},
			Hostname: func() (string, error) { return hostname, nil },
		},
	}
}
