package packages

import (
	"net/netip"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func TestPoolAddressRefusalTwins(t *testing.T) {
	for _, test := range []struct{ name, bad, good, code, field, message string }{
		{"canonical", "192.0.2.81/28", "192.0.2.80/28", "api.value", ".addresses[0]", "not a canonical"},
		{"reversed", "192.0.2.90-192.0.2.80", "192.0.2.80-192.0.2.90", "api.value", ".addresses[0]", "ends before"},
		{"mixed", "192.0.2.80-2001:db8::90", "192.0.2.80-192.0.2.90", "api.value", ".addresses[0]", "mixes address families"},
		{"family", "2001:db8::80-2001:db8::90", "192.0.2.80-192.0.2.90", "api.invariant", ".addresses[0]", "not of the ipv4 family"},
		{"outside", "203.0.113.80-203.0.113.90", "192.0.2.80-192.0.2.90", "api.invariant", ".addresses[0]", "outside every machine network"},
		{"node", "192.0.2.10-192.0.2.15", "192.0.2.11-192.0.2.15", "api.invariant", ".addresses[0]", "held by Machine/node"},
		{"next hop", "192.0.2.1-192.0.2.5", "192.0.2.2-192.0.2.5", "api.invariant", ".addresses[0]", "next hop"},
		{"network", "192.0.2.0-192.0.2.0", "192.0.2.2-192.0.2.2", "api.invariant", ".addresses[0]", "network or broadcast"},
		{"broadcast", "192.0.2.254-192.0.2.255", "192.0.2.254-192.0.2.254", "api.invariant", ".addresses[0]", "network or broadcast"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := facts()
			if issues := admitPools(addons.Instance{Config: config(pool("services", test.good))}, f); len(issues) != 0 {
				t.Fatalf("passing twin: %+v", issues)
			}
			issues := admitPools(addons.Instance{Config: config(pool("services", test.bad))}, f)
			for _, issue := range issues {
				if issue.Code == test.code && issue.Field == ".pools[0]"+test.field && strings.Contains(issue.Message, test.message) && issue.Remediation != "" {
					return
				}
			}
			t.Fatalf("missing refusal: %+v", issues)
		})
	}
}

func TestPoolSpanningAndOverlapTwins(t *testing.T) {
	f := facts()
	f.MachineNetworks = append(f.MachineNetworks, netip.MustParsePrefix("198.51.100.0/24"))
	f.NodeNetworks[0].Interfaces = append(f.NodeNetworks[0].Interfaces, addons.NodeInterface{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.10/24")}})
	for _, test := range []struct {
		name                 string
		bad, good            []api.Value
		code, field, message string
	}{
		{"spanning", []api.Value{pool("services", "192.0.2.80-192.0.2.90", "198.51.100.80-198.51.100.90")}, []api.Value{pool("services", "192.0.2.80-192.0.2.90"), pool("secondary", "198.51.100.80-198.51.100.90")}, "api.invariant", ".pools[0].addresses", "spans"},
		{"overlap", []api.Value{pool("a", "192.0.2.80-192.0.2.90"), pool("b", "192.0.2.90-192.0.2.95")}, []api.Value{pool("a", "192.0.2.80-192.0.2.90"), pool("b", "192.0.2.91-192.0.2.95")}, "api.duplicate", ".pools[1].addresses[0]", "overlaps pool a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if issues := admitPools(addons.Instance{Config: config(test.good...)}, f); len(issues) != 0 {
				t.Fatalf("passing twin: %+v", issues)
			}
			issues := admitPools(addons.Instance{Config: config(test.bad...)}, f)
			for _, issue := range issues {
				if issue.Code == test.code && issue.Field == test.field && strings.Contains(issue.Message, test.message) && issue.Remediation != "" {
					return
				}
			}
			t.Fatalf("missing refusal: %+v", issues)
		})
	}
}

// Without l2.interface every node announces the pool, so each needs an
// address on the pool's network.
func TestEveryPoolAnnouncerIsOnThePoolNetwork(t *testing.T) {
	f := facts()
	f.MachineNetworks = append(f.MachineNetworks, netip.MustParsePrefix("198.51.100.0/24"))
	f.NodeNetworks = append(f.NodeNetworks, addons.NodeNetwork{Machine: "other", Interfaces: []addons.NodeInterface{{Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.20/24")}}}})
	issues := admitPools(addons.Instance{Config: config(pool("services", "192.0.2.80-192.0.2.90"))}, f)
	want := "node other of OpenShiftCluster/cluster has no address in 192.0.2.0/24, the network of pool services, so it cannot answer for the pool on layer 2"
	if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != ".pools[0].addresses[0]" || issues[0].Message != want || issues[0].Remediation == "" {
		t.Fatalf("issues: %+v", issues)
	}
	f.NodeNetworks[1].Interfaces[0].Addresses = append(f.NodeNetworks[1].Interfaces[0].Addresses, netip.MustParsePrefix("192.0.2.20/24"))
	if issues := admitPools(addons.Instance{Config: config(pool("services", "192.0.2.80-192.0.2.90"))}, f); len(issues) != 0 {
		t.Fatalf("an announcer on the pool network refused: %+v", issues)
	}
}
