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

func eth(name string) api.Value { return m("name", name, "type", "ethernet") }
func vlanOn(name, base string) api.Value {
	return m("name", name, "type", "vlan", "vlan", m("base-iface", base, "id", api.IntegerValue("100")))
}
func assign(name, address, iface string) api.Value {
	return m("name", name, "address", address, "interface", iface)
}
func addressedNetwork(name, bridge, iface, hostAddresses, vlan string) api.Value {
	uplink := m("bridge", bridge)
	if iface != "" {
		uplink = uplink.With("interface", api.StringValue(iface))
	}
	if hostAddresses != "" {
		uplink = uplink.With("hostAddresses", api.StringValue(hostAddresses))
	}
	n := secondaryNetwork().With("name", api.StringValue(name)).With("uplink", uplink)
	if vlan != "" {
		n = n.With("vlan", api.IntegerValue(vlan))
	}
	return n
}

func uplinkLab(interfaces []api.Value, addresses []api.Value, routes ...api.Value) (api.Object, api.Catalog) {
	cluster, c := fixture("libvirt", 1, false)
	bound, _ := c.Find(api.Machine, "node-0")
	spec := bound.Spec().WithPath(m("interfaces", list(interfaces...), "routes", m("config", list(routes...))), "network", "inline", "nmstate").WithPath(list(addresses...), "network", "addresses")
	objects := []api.Object{}
	for _, o := range c.Objects() {
		if o.Kind() == api.Machine && o.Name() == "node-0" {
			o = o.WithSpec(spec)
		}
		objects = append(objects, o)
	}
	return cluster, api.NewCatalog(objects)
}

func installRoute(iface string) api.Value {
	return m("destination", "0.0.0.0/0", "next-hop-interface", iface, "next-hop-address", "192.0.2.1")
}

func TestAddressedAndCarriedUplinkAdmission(t *testing.T) {
	const networks = "$.spec.networking.secondaryNetworks["
	plain := func() ([]api.Value, []api.Value, []api.Value) {
		return []api.Value{eth("eth0"), eth("eth1"), vlanOn("eth1.10", "eth1")}, []api.Value{assign("primary", "192.0.2.10/24", "eth0")}, []api.Value{installRoute("eth0")}
	}
	addressed := func() ([]api.Value, []api.Value, []api.Value) {
		interfaces, addresses, routes := plain()
		return interfaces, append(addresses, assign("tenant", "198.51.100.10/24", "eth1.10")), routes
	}
	installVLAN := func() ([]api.Value, []api.Value, []api.Value) {
		return []api.Value{eth("eth0"), vlanOn("eth0.100", "eth0"), eth("eth1")}, []api.Value{assign("primary", "192.0.2.10/24", "eth0.100")}, []api.Value{installRoute("eth0.100")}
	}
	for _, test := range []struct {
		name     string
		lab      func() ([]api.Value, []api.Value, []api.Value)
		networks []api.Value
		field    string
		message  string
		remedy   string
	}{
		{"bridge over an addressed vlan", addressed, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "bridge", "")}, "", "", ""},
		{"none over an addressless vlan", plain, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "none", "")}, "", "", ""},
		{"bridge over an addressless vlan", plain, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "bridge", "")}, "", "", ""},
		{"two networks one bridge", addressed, []api.Value{addressedNetwork("a", "br-tenant", "eth1.10", "bridge", "10"), addressedNetwork("b", "br-tenant", "eth1.10", "bridge", "20")}, "", "", ""},
		{"addressed uplink without placement", addressed, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "", "")}, networks + "0].uplink.interface", "uplink eth1.10 carries an address on Machine/node-0", "set uplink.hostAddresses: bridge to move its addresses onto bridge br-tenant"},
		{"addressed uplink with none", addressed, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "none", "")}, networks + "0].uplink.interface", "carries an address", "uplink.hostAddresses: bridge"},
		{"bridge on br-ex", plain, []api.Value{addressedNetwork("tenant", "br-ex", "", "bridge", "")}, networks + "0].uplink.hostAddresses", "asks br-ex to carry host addresses", "dedicated bridge"},
		{"shared bridge disagrees", addressed, []api.Value{addressedNetwork("a", "br-tenant", "eth1.10", "bridge", "10"), addressedNetwork("b", "br-tenant", "eth1.10", "", "20")}, networks + "1].uplink.hostAddresses", "secondary networks a and b share bridge br-tenant but differ in uplink.hostAddresses", "same uplink.hostAddresses"},
		{"bridge over dhcp", func() ([]api.Value, []api.Value, []api.Value) {
			interfaces, addresses, routes := plain()
			interfaces[2] = interfaces[2].With("ipv4", m("enabled", true, "dhcp", true))
			return interfaces, addresses, routes
		}, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "bridge", "")}, networks + "0].uplink.interface", "addressed by DHCP or autoconf", "declare static addresses"},
		{"bridge over a routed uplink", func() ([]api.Value, []api.Value, []api.Value) {
			interfaces, addresses, routes := addressed()
			return interfaces, addresses, append(routes, m("destination", "203.0.113.0/24", "next-hop-interface", "eth1.10", "next-hop-address", "198.51.100.1"))
		}, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1.10", "bridge", "")}, networks + "0].uplink.interface", "next-hop interface of a route on Machine/node-0", "remove the route"},
		{"tag on br-ex over a vlan install interface", installVLAN, []api.Value{addressedNetwork("tenant", "br-ex", "", "", "10")}, networks + "0].vlan", "network tenant tags VLAN 10 on br-ex, which node node-0 builds on VLAN interface eth0.100", "its own bridge over an addressless VLAN interface"},
		{"untagged on br-ex over a vlan install interface", installVLAN, []api.Value{addressedNetwork("tenant", "br-ex", "", "", "")}, "", "", ""},
		{"tag on br-ex over an untagged install interface", plain, []api.Value{addressedNetwork("tenant", "br-ex", "", "", "10")}, "", "", ""},
		{"uplink is the base of the install vlan", installVLAN, []api.Value{addressedNetwork("tenant", "br-tenant", "eth0", "", "")}, networks + "0].uplink.interface", "uplink eth0 of network tenant carries interface eth0.100 on node node-0", "addressless VLAN interface of eth0"},
		{"uplink is the base of another vlan", plain, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1", "", "")}, networks + "0].uplink.interface", "uplink eth1 of network tenant carries interface eth1.10 on node node-0", "addressless VLAN interface of eth1"},
		{"uplink is a bond port", func() ([]api.Value, []api.Value, []api.Value) {
			return []api.Value{eth("eth0"), eth("eth1"), m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("eth1")))}, []api.Value{assign("primary", "192.0.2.10/24", "eth0")}, []api.Value{installRoute("eth0")}
		}, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1", "", "")}, networks + "0].uplink.interface", "uplink eth1 of network tenant carries interface bond0 on node node-0", "addressless VLAN interface"},
		{"uplink names its bond controller", func() ([]api.Value, []api.Value, []api.Value) {
			return []api.Value{eth("eth0"), eth("eth1").With("controller", api.StringValue("bond0")), m("name", "bond0", "type", "bond")}, []api.Value{assign("primary", "192.0.2.10/24", "eth0")}, []api.Value{installRoute("eth0")}
		}, []api.Value{addressedNetwork("tenant", "br-tenant", "eth1", "", "")}, networks + "0].uplink.interface", "carries interface bond0", "addressless VLAN interface"},
		{"bond uplink over its own vlan-free ports", func() ([]api.Value, []api.Value, []api.Value) {
			return []api.Value{eth("eth0"), m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("eth1", "eth2"))), eth("eth1"), eth("eth2")}, []api.Value{assign("primary", "192.0.2.10/24", "eth0")}, []api.Value{installRoute("eth0")}
		}, []api.Value{addressedNetwork("tenant", "br-tenant", "bond0", "", "")}, "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			interfaces, addresses, routes := test.lab()
			cluster, c := uplinkLab(interfaces, addresses, routes...)
			o := NormalizeNetworkIntent(cluster.WithSpec(cluster.Spec().WithPath(list(test.networks...), "networking", "secondaryNetworks")))
			issues := ValidateNetworkIntent(o, c)
			if test.field == "" {
				if len(issues) > 0 {
					t.Fatal(issues)
				}
				return
			}
			for _, issue := range issues {
				if issue.Field == test.field && strings.Contains(issue.Message, test.message) && strings.Contains(issue.Remediation, test.remedy) {
					return
				}
			}
			t.Fatalf("want %s %q / %q in %+v", test.field, test.message, test.remedy, issues)
		})
	}
}
