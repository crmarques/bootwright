package openshift

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func secondaryNetwork() api.Value {
	return m("name", "tenant", "topology", "Localnet", "uplink", m("bridge", "br-ex"), "namespaceSelector", m("matchLabels", m("tenant", "yes")))
}
func TestSecondaryNetworkAdmission(t *testing.T) {
	cluster, c := fixture("libvirt", 1, false)
	for _, test := range []struct {
		name  string
		value api.Value
		want  string
	}{
		{"valid", secondaryNetwork(), ""},
		{"reserved", secondaryNetwork().With("name", api.StringValue("physnet")), "reserved"},
		{"bridge punctuation", secondaryNetwork().With("uplink", m("bridge", "br@tenant", "interface", "eth1")), "invalid or reserved bridge"},
		{"bridge control", secondaryNetwork().With("uplink", m("bridge", "br\ttenant", "interface", "eth1")), "invalid or reserved bridge"},
		{"bridge unicode", secondaryNetwork().With("uplink", m("bridge", "br-tenanté", "interface", "eth1")), "invalid or reserved bridge"},
		{"selector", secondaryNetwork().With("namespaceSelector", m("matchLabels", m())), "labels"},
		{"mixed uplink", secondaryNetwork().With("uplink", m("bridge", "br-ex", "interface", "eth1")), "br-ex alone"},
		{"missing uplink", secondaryNetwork().With("uplink", m("bridge", "br-tenant", "interface", "eth1")), "missing"},
		{"disabled subnet", secondaryNetwork().With("ipam", m("subnets", api.StringList("198.51.100.0/24"))), "disabled IPAM"},
		{"enabled no subnet", secondaryNetwork().With("ipam", m("mode", "Enabled")), "one or two"},
		{"repeated family", secondaryNetwork().With("ipam", m("mode", "Enabled", "subnets", api.StringList("198.51.100.0/24", "203.0.113.0/24"))), "repeat"},
		{"outside exclusion", secondaryNetwork().With("ipam", m("mode", "Enabled", "subnets", api.StringList("198.51.100.0/24"), "excludeSubnets", api.StringList("203.0.113.0/24"))), "outside"},
		{"ipv6 mtu", secondaryNetwork().With("mtu", api.IntegerValue("1200")).With("ipam", m("mode", "Enabled", "subnets", api.StringList("2001:db8::/64"))), "below 1280"},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := cluster.WithSpec(cluster.Spec().WithPath(list(test.value), "networking", "secondaryNetworks"))
			o = NormalizeNetworkIntent(o)
			again := NormalizeNetworkIntent(o)
			if !o.Value().Equal(again.Value()) {
				t.Fatal("not idempotent")
			}
			issues := ValidateNetworkIntent(o, c)
			if test.want == "" {
				if len(issues) > 0 {
					t.Fatal(issues)
				}
				return
			}
			for _, issue := range issues {
				if strings.Contains(issue.Message, test.want) {
					if issue.Remediation == "" {
						t.Fatal(issue)
					}
					return
				}
			}
			t.Fatal(issues)
		})
	}
}
func TestOVNMTUAndProviderRequirement(t *testing.T) {
	cluster, c := fixture("libvirt", 1, false)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("mtu", api.IntegerValue("1401")), "networking", "ovnKubernetesConfig"))
	if issues := ValidateNetworkIntent(cluster, c); len(issues) != 1 {
		t.Fatal(issues)
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(list(secondaryNetwork()), "networking", "secondaryNetworks"))
	if issues := ValidateNetworkProviders(cluster, c, addons.Registry{}); len(issues) != 1 {
		t.Fatal(issues)
	}
}

func TestDefaultRouteInterfaceUsesClusterFamily(t *testing.T) {
	native := m("routes", m("config", list(m("destination", "::/0", "next-hop-interface", "eth1"), m("destination", "0.0.0.0/0", "next-hop-interface", "eth0"))))
	if got := defaultRouteInterface(native, 32); got != "eth0" {
		t.Fatal(got)
	}
	if got := defaultRouteInterface(native, 128); got != "eth1" {
		t.Fatal(got)
	}
}

func TestSecondaryNetworkReservedNames(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	names := []string{"lo", "br-ex", "br-ext", "br-int", "br-local", "br-nexthop", "br0", "ext-vxlan", "ext", "int", "tun0", "genev_sys_1", "k8s-test", "ovn-k8s-test", "patch-br-test", "vxlan_sys_1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			bound, _ := catalog.Find(api.Machine, "node-0")
			native := m("interfaces", list(m("name", name, "type", "ethernet")))
			n := secondaryNetwork().With("uplink", m("interface", name, "bridge", "br-tenant"))
			issues := validateNetworkUplink(n, 0, bound, native, "eth0")
			if len(issues) != 1 || issues[0].Field != "$.spec.networking.secondaryNetworks[0].uplink.interface" {
				t.Fatalf("reserved interface: %+v", issues)
			}
			n = secondaryNetwork().With("uplink", m("interface", "eth1", "bridge", name))
			if name == "br-ex" {
				n = secondaryNetwork()
			}
			o := NormalizeNetworkIntent(cluster.WithSpec(cluster.Spec().WithPath(list(n), "networking", "secondaryNetworks")))
			issues = ValidateNetworkIntent(o, catalog)
			found := false
			for _, issue := range issues {
				if issue.Field == "$.spec.networking.secondaryNetworks[0].uplink.bridge" {
					found = true
				}
			}
			if found != (name != "br-ex") {
				t.Fatalf("reserved bridge: %+v", issues)
			}
		})
	}
	bound, _ := catalog.Find(api.Machine, "node-0")
	native := m("interfaces", list(m("name", "eth1", "type", "ethernet")))
	n := secondaryNetwork().With("uplink", m("interface", "eth1", "bridge", "br-tenant"))
	if issues := validateNetworkUplink(n, 0, bound, native, "eth0"); len(issues) != 0 {
		t.Fatalf("passing dedicated uplink: %+v", issues)
	}
}
