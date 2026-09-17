package machine

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A Machine's IPs are the addresses it declares that are IPs: a DNS contact is
// not one, a host IP loses its prefix, and one address declared twice is
// reported once. The order is the Machine's own.
func TestDeclaredIPsAreEveryIPInDeclaredOrderWithoutRepetition(t *testing.T) {
	node := object(api.Machine, "node", m("network", m("addresses", list(
		m("name", "fqdn", "address", "node.lab.example.test"),
		m("name", "ssh", "address", "198.51.100.20/24"),
		m("name", "storage", "address", "192.0.2.20"),
		m("name", "mirror", "address", "198.51.100.20/24"),
		m("name", "six", "address", "2001:db8::20/64")))))
	want := []string{"198.51.100.20", "192.0.2.20", "2001:db8::20"}
	if found := IPAddresses(node); !slices.Equal(found, want) {
		t.Fatalf("declared IPs = %v, want %v", found, want)
	}
}

// A Machine that declares no address, or only a name, reports no IP rather
// than an empty string that would read as one.
func TestAMachineDeclaringNoIPReportsNone(t *testing.T) {
	for name, spec := range map[string]api.Value{
		"no network":  m("os", m("provided", true)),
		"no address":  m("network", m("addresses", list())),
		"a name only": m("network", m("addresses", list(m("name", "fqdn", "address", "node.lab.example.test")))),
		"not an IP":   m("network", m("addresses", list(m("name", "ssh", "address", "192.0.2.300")))),
	} {
		t.Run(name, func(t *testing.T) {
			if found := IPAddresses(object(api.Machine, "node", spec)); len(found) != 0 {
				t.Fatalf("declared IPs = %v", found)
			}
		})
	}
}
