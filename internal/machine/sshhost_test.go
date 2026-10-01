package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Two SSH endpoints are one host exactly when their address and port are, as
// a known_hosts token names a host: an IP in any spelling of its value is one
// address, a DNS name is one whatever its case or final dot, and one address
// at two ports is two hosts.
func TestAnSSHHostIsItsAddressAndPort(t *testing.T) {
	for name, tc := range map[string]struct {
		first, second string
		firstPort     int
		secondPort    int
		same          bool
	}{
		"one address and port":          {"192.0.2.10", "192.0.2.10", 22, 22, true},
		"one address at two ports":      {"192.0.2.10", "192.0.2.10", 22, 2222, false},
		"two addresses at one port":     {"192.0.2.10", "192.0.2.20", 22, 22, false},
		"an IPv4-mapped address":        {"::ffff:192.0.2.10", "192.0.2.10", 22, 22, true},
		"two spellings of one IPv6":     {"fd00:0:0::10", "FD00::10", 2222, 2222, true},
		"one name in two cases":         {"Services.Example.Test.", "services.example.test", 22, 22, true},
		"a name beside its own address": {"services.example.test", "192.0.2.10", 22, 22, false},
	} {
		t.Run(name, func(t *testing.T) {
			if same := SSHHost(tc.first, tc.firstPort) == SSHHost(tc.second, tc.secondPort); same != tc.same {
				t.Fatalf("%s and %s read as one host = %t, want %t", SSHHost(tc.first, tc.firstPort), SSHHost(tc.second, tc.secondPort), same, tc.same)
			}
		})
	}
	if host := SSHHost("fd00::10", 2222); host != "[fd00::10]:2222" {
		t.Fatalf("an IPv6 SSH host reads %q", host)
	}
}

// An SSH endpoint is the controller at port 22 on a loopback address, on
// localhost or on an address the controller Machine declares, in any spelling;
// at another port, or at an address the controller does not declare, it is a
// host of its own.
func TestAnSSHEndpointIsTheControllerOnlyAtItsOwnAddresses(t *testing.T) {
	controller := object(api.Machine, "controller", m("network", m("addresses", list(
		m("name", "fqdn", "address", "controller.lab.example.test"),
		m("name", "lab", "address", "192.0.2.1/24"),
		m("name", "six", "address", "fd00::1")))))
	for name, tc := range map[string]struct {
		address    string
		port       int
		controller bool
	}{
		"a declared address":           {"192.0.2.1", 22, true},
		"a declared IPv6 spelling":     {"fd00:0::1", 22, true},
		"a declared name":              {"Controller.Lab.Example.Test.", 22, true},
		"loopback":                     {"127.0.0.1", 22, true},
		"another loopback address":     {"127.0.1.1", 22, true},
		"IPv6 loopback":                {"::1", 22, true},
		"localhost":                    {"LOCALHOST", 22, true},
		"a declared address elsewhere": {"192.0.2.1", 2222, false},
		"a forwarded loopback port":    {"127.0.0.1", 2222, false},
		"another host":                 {"192.0.2.10", 22, false},
	} {
		t.Run(name, func(t *testing.T) {
			if reached := ReachesController(controller, tc.address, tc.port); reached != tc.controller {
				t.Fatalf("%s reaches the controller = %t, want %t", SSHHost(tc.address, tc.port), reached, tc.controller)
			}
		})
	}
	if ReachesController(api.Object{}, "192.0.2.1", 22) {
		t.Fatal("an undeclared controller claimed an address it never declared")
	}
}
