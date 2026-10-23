package containercluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestAgentNodeNetworkRefusals(t *testing.T) {
	_, catalog := fixture("libvirt", 1, false)
	node, _ := catalog.Find(api.Machine, "node-0")
	native, errors := machine.ComposeNetwork(node, catalog)
	if len(errors) > 0 {
		t.Fatal(errors)
	}
	for _, tc := range []struct {
		name  string
		value api.Value
		want  string
	}{
		{"OVN", native.With("ovn", m()), "ovn or ovs-db"}, {"OVS DB", native.With("ovs-db", m()), "ovn or ovs-db"},
		{"OVS bridge", native.With("interfaces", list(m("name", "bridge", "type", "ovs-bridge"))), "unqualified bridge"},
		{"Linux bridge", native.With("interfaces", list(m("name", "bridge", "type", "linux-bridge"))), "unqualified bridge"},
		{"reserved", native.With("interfaces", list(m("name", "br-ex", "type", "ethernet"))), "reserved interface"},
		{"reserved prefix", native.With("interfaces", list(m("name", "ovn-k8s-test", "type", "ethernet"))), "reserved interface"},
		{"different route", native.With("routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "other")))), "not on its default-route"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := NodeNetworkIssues(node, catalog, tc.value)
			if len(issues) == 0 || !strings.Contains(issues[0].Message, tc.want) || issues[0].Code != "api.invariant" || issues[0].Remediation == "" {
				t.Fatal(issues)
			}
		})
	}
	if issues := NodeNetworkIssues(node, catalog, native); len(issues) > 0 {
		t.Fatal(issues)
	}
}

func TestAgentNodeDefaultRouteAmbiguity(t *testing.T) {
	_, catalog := fixture("libvirt", 1, false)
	node, _ := catalog.Find(api.Machine, "node-0")
	native, errors := machine.ComposeNetwork(node, catalog)
	if len(errors) > 0 {
		t.Fatal(errors)
	}
	route := func(iface string, metric int) api.Value {
		return m("destination", "0.0.0.0/0", "next-hop-interface", iface, "metric", metric)
	}
	for _, tc := range []struct {
		name      string
		routes    api.Value
		ambiguous bool
	}{
		{"lower metric last", list(route("eth0", 600), route("eth1", 100)), true},
		{"lower metric first", list(route("eth1", 100), route("eth0", 600)), true},
		{"same interface", list(route("eth0", 600), route("eth0", 100)), false},
		{"absent competing route", list(route("eth0", 600), route("eth1", 100).With("state", api.StringValue("absent"))), false},
		{"other address family", list(route("eth0", 600), route("eth1", 100).With("destination", api.StringValue("::/0"))), false},
		{"different table", list(route("eth0", 600), route("eth1", 100).With("table-id", api.IntegerValue("100"))), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := native.With("routes", m("config", tc.routes))
			issues := NodeNetworkIssues(node, catalog, value)
			if tc.ambiguous {
				if len(issues) != 1 || !strings.Contains(issues[0].Message, "default routes on multiple interfaces") || issues[0].Remediation == "" {
					t.Fatal(issues)
				}
				if got := defaultRouteInterface(value, 32); got != "" {
					t.Fatalf("ambiguous default selected %q", got)
				}
			} else if len(issues) != 0 {
				t.Fatal(issues)
			}
		})
	}
}
