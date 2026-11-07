package openshift

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func segmentedLab(networks ...api.Value) (api.Object, api.Catalog) {
	return segmentedNodes(1, networks...)
}

func segmentedNodes(count int, networks ...api.Value) (api.Object, api.Catalog) {
	cluster, c := fixture("libvirt", count, false)
	objects := []api.Object{}
	for _, o := range c.Objects() {
		switch {
		case o.Kind() == api.InfraProvider:
			o = obj(api.InfraProvider, "provider", m("libvirt", m(), "segments", list(m("name", "trunk", "untaggedNetworkRef", "lan", "taggedNetworkRefs", api.StringList("tenant"), "libvirt", m("bridge", "br-trunk")))))
		case o.Kind() == api.Machine:
			interfaces := list(eth("eth0"), eth("eth1"), eth("eth2"), m("name", "eth2.200", "type", "vlan", "vlan", m("base-iface", "eth2", "id", api.IntegerValue("200"))))
			o = o.WithSpec(o.Spec().WithPath(m("interfaces", interfaces, "routes", m("config", list(installRoute("eth0")))), "network", "inline", "nmstate"))
		}
		objects = append(objects, o)
	}
	objects = append(objects, obj(api.Network, "tenant", m("provided", true, "vlan", api.IntegerValue("200"), "ipv4", m("cidr", "203.0.113.0/24"))))
	return NormalizeNetworkIntent(cluster.WithSpec(cluster.Spec().WithPath(list(networks...), "networking", "secondaryNetworks"))), api.NewCatalog(objects)
}

func enabled(n api.Value, subnet string) api.Value {
	return n.With("ipam", m("mode", "Enabled", "subnets", api.StringList(subnet)))
}

func excluding(n api.Value, subnets ...string) api.Value {
	return n.With("ipam", n.Get("ipam").With("excludeSubnets", api.StringList(subnets...)))
}

func TestASecondaryNetworkCarriesTheNetworkOfItsBridgeInterfaceSegment(t *testing.T) {
	const base = "$.spec.networking.secondaryNetworks[0]"
	for _, test := range []struct {
		name               string
		network            api.Value
		field, message, to string
	}{
		{"untagged on the trunk", excluding(enabled(addressedNetwork("tenant", "br-tenant", "eth1", "", ""), "192.0.2.0/24"), "192.0.2.10/32"), "", "", ""},
		{"a node address left to pods", enabled(addressedNetwork("tenant", "br-tenant", "eth1", "", ""), "192.0.2.0/24"), base + ".ipam.excludeSubnets", "IPAM subnet 192.0.2.0/24 of secondary network tenant spans Network/lan, so pods can take 192.0.2.10, held by node node-0 (Machine/node-0)", "add 192.0.2.10/32, or a range holding it, to ipam.excludeSubnets"},
		{"untagged subnet of another Network", enabled(addressedNetwork("tenant", "br-tenant", "eth1", "", ""), "203.0.113.0/24"), base + ".ipam.subnets[0]", "IPAM subnet 203.0.113.0/24 of secondary network tenant is not the ipv4 cidr of Network/lan, which it carries through interface eth1 of node node-0 (Machine/node-0), NIC eth1 on segment trunk", "192.0.2.0/24"},
		{"tagged by vlan", enabled(addressedNetwork("tenant", "br-tenant", "eth1", "", "200"), "203.0.113.0/24"), "", "", ""},
		{"a vlan the trunk does not carry", addressedNetwork("tenant", "br-tenant", "eth1", "", "300"), base + ".vlan", "carries no tagged Network with vlan 300", "taggedNetworkRefs"},
		{"tagged by its VLAN interface", enabled(addressedNetwork("tenant", "br-tenant", "eth2.200", "", ""), "203.0.113.0/24"), "", "", ""},
		{"a vlan over a VLAN interface", addressedNetwork("tenant", "br-tenant", "eth2.200", "", "200"), base + ".vlan", "tags VLAN 200 on interface eth2.200 of node node-0 (Machine/node-0), which VLAN interface eth2.200 already tags", "remove vlan"},
		{"a vlan on br-ex", addressedNetwork("tenant", "br-ex", "", "", "200"), "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster, c := segmentedLab(test.network)
			issues := ValidateNetworkIntent(cluster, c)
			if test.field == "" {
				if len(issues) != 0 {
					t.Fatalf("%+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Field != test.field || !strings.Contains(issues[0].Message, test.message) || !strings.Contains(issues[0].Remediation, test.to) {
				t.Fatalf("%+v", issues)
			}
		})
	}
}

func TestANodeOverridesTheBridgeInterfaceOfADedicatedBridge(t *testing.T) {
	overridden := func(cluster api.Object, entries ...api.Value) api.Object {
		nodes := cluster.Spec().Get("nodes").Items()
		nodes[0] = nodes[0].With("bridgeInterfaces", list(entries...))
		return cluster.WithSpec(cluster.Spec().With("nodes", list(nodes...)))
	}
	cluster, c := segmentedLab(addressedNetwork("tenant", "br-tenant", "eth0", "", ""))
	if issues := ValidateNetworkIntent(overridden(cluster, m("networkRef", "tenant", "interface", "eth1")), c); len(issues) != 0 {
		t.Fatalf("an override of the install interface: %+v", issues)
	}
	if got := BridgeInterface(overridden(cluster, m("networkRef", "tenant", "interface", "eth1")).Spec().Get("nodes").Items()[0], cluster.Spec().Get("networking", "secondaryNetworks").Items()[0]); got != "eth1" {
		t.Fatal(got)
	}
	cluster, c = segmentedLab(addressedNetwork("tenant", "br-ex", "", "", ""))
	issues := ValidateNetworkIntent(overridden(cluster, m("networkRef", "tenant", "interface", "eth1")), c)
	if len(issues) != 1 || issues[0].Field != "$.spec.nodes[0].bridgeInterfaces[0].networkRef" || !strings.Contains(issues[0].Message, "shares br-ex") {
		t.Fatalf("an override of br-ex: %+v", issues)
	}
	cluster, c = segmentedLab(addressedNetwork("a", "br-tenant", "eth1", "", "200"), addressedNetwork("b", "br-tenant", "eth1", "", ""))
	issues = ValidateNetworkIntent(overridden(cluster, m("networkRef", "a", "interface", "eth2.200")), c)
	found := false
	for _, issue := range issues {
		found = found || issue.Field == "$.spec.nodes[0].bridgeInterfaces" && strings.Contains(issue.Message, "different bridge interfaces")
	}
	if !found {
		t.Fatalf("a split shared bridge: %+v", issues)
	}
}

func TestAnOverriddenBridgeInterfaceIsRefusedAtItsNode(t *testing.T) {
	cluster, c := segmentedLab(addressedNetwork("tenant", "br-tenant", "eth1", "", ""))
	nodes := cluster.Spec().Get("nodes").Items()
	nodes[0] = nodes[0].With("bridgeInterfaces", list(m("networkRef", "tenant", "interface", "eth0")))
	issues := ValidateNetworkIntent(cluster.WithSpec(cluster.Spec().With("nodes", list(nodes...))), c)
	if len(issues) != 2 || issues[1].Field != "$.spec.nodes[0].bridgeInterfaces[0].interface" || !strings.Contains(issues[1].Message, "the install interface") {
		t.Fatalf("%+v", issues)
	}
}

func TestAddonFactsNameTheNetworksEachInterfaceReaches(t *testing.T) {
	cluster, c := segmentedLab()
	catalog := api.NewCatalog(append(c.Objects(), cluster))
	facts := AddonFacts(catalog, cluster.Name())
	if len(facts.Networks) != 2 || facts.Networks[0].Name != "lan" || facts.Networks[1].Name != "tenant" {
		t.Fatalf("networks = %+v", facts.Networks)
	}
	reached := map[string][]string{}
	for _, nic := range facts.NodeNetworks[0].Interfaces {
		reached[nic.Name] = nic.Networks
	}
	if strings.Join(reached["eth0"], ",") != "lan" || strings.Join(reached["eth1"], ",") != "lan" || strings.Join(reached["eth2.200"], ",") != "tenant" {
		t.Fatalf("reached = %v", reached)
	}
}

func overrideOn(cluster api.Object, node int, entries ...api.Value) api.Object {
	nodes := cluster.Spec().Get("nodes").Items()
	nodes[node] = nodes[node].With("bridgeInterfaces", list(entries...))
	return cluster.WithSpec(cluster.Spec().With("nodes", list(nodes...)))
}

func TestAnOverrideCannotPutOneInterfaceInTwoBridges(t *testing.T) {
	cluster, c := segmentedLab(addressedNetwork("a", "br-a", "eth1", "", ""), addressedNetwork("b", "br-b", "eth2.200", "", ""))
	if issues := ValidateNetworkIntent(cluster, c); len(issues) != 0 {
		t.Fatalf("distinct interfaces: %+v", issues)
	}
	issues := ValidateNetworkIntent(overrideOn(cluster, 0, m("networkRef", "b", "interface", "eth1")), c)
	found := false
	for _, issue := range issues {
		found = found || issue.Field == "$.spec.nodes[0].bridgeInterfaces[0].interface" && strings.Contains(issue.Message, "node node-0 assigns interface eth1 to bridge br-a of secondary network a and to bridge br-b of secondary network b") && strings.Contains(issue.Remediation, "secondary network b on node node-0")
	}
	if !found {
		t.Fatalf("%+v", issues)
	}
}

func TestASecondaryNetworkReachesOneNetworkOnEveryNode(t *testing.T) {
	cluster, c := segmentedNodes(2, addressedNetwork("tenant", "br-tenant", "eth1", "", ""))
	if issues := ValidateNetworkIntent(cluster, c); len(issues) != 0 {
		t.Fatalf("one Network: %+v", issues)
	}
	issues := ValidateNetworkIntent(overrideOn(cluster, 1, m("networkRef", "tenant", "interface", "eth2.200")), c)
	if len(issues) != 1 || issues[0].Field != "$.spec.networking.secondaryNetworks[0].bridgeMapping" ||
		!strings.Contains(issues[0].Message, "secondary network tenant reaches Network/lan on node node-0 through segment trunk (spec.segments[0] of InfraProvider/provider), and Network/tenant through interface eth2.200 of node node-1") ||
		!strings.Contains(issues[0].Remediation, "segment that carries Network/lan, or override the bridge interface of node node-1 at spec.nodes[1].bridgeInterfaces") {
		t.Fatalf("%+v", issues)
	}
}
