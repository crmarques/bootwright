package openshift

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
)

func TestAddonFactsShareEndpointOwnershipClaims(t *testing.T) {
	cluster, catalog := fixture("libvirt", 3, false)
	cluster, _ = Normalize(cluster, catalog)
	other := obj(api.Machine, "other", m("network", m("addresses", list(m("address", "192.0.2.90")))))
	dns := obj(api.DNSServer, "dns", m("management", "managed", "bindAddress", "192.0.2.91"))
	provider := managedNetwork("network-host", "lab", "192.0.2.1/24", "virbr-host")
	objects := append(withoutNetwork(catalog, "lan"), cluster, other, dns, provider)
	facts := AddonFacts(api.NewCatalog(objects), cluster.Name())
	if facts.Release != "4.21.15" || facts.Family != "ipv4" || len(facts.Nodes) != 3 || len(facts.NodeNetworks) != 3 {
		t.Fatalf("cluster facts: %+v", facts)
	}
	if !slices.Contains(facts.MachineNetworks, netip.MustParsePrefix("192.0.2.0/24")) {
		t.Fatal(facts.MachineNetworks)
	}
	holders := map[string]bool{}
	for _, holder := range facts.Reserved {
		holders[holder.Holder] = true
	}
	for _, want := range []string{"Machine/other", "DNSServer/dns", "Network/network-host"} {
		if !holders[want] {
			t.Fatalf("missing %s: %+v", want, facts.Reserved)
		}
	}
	for _, claim := range addressClaims(api.NewCatalog(objects)) {
		if claim.Object.Kind() == api.OpenShiftCluster && claim.Object.Name() == cluster.Name() || claim.Object.Kind() == api.Machine && slices.Contains(facts.Nodes, claim.Object.Name()) {
			continue
		}
		found := false
		for _, holder := range facts.Reserved {
			if holder.Addr == claim.Address && holder.Holder == claim.Object.Identity() {
				found = true
			}
		}
		if !found {
			t.Fatalf("claim missing from package facts: %+v", claim)
		}
	}
	slices.Reverse(objects)
	if got := AddonFacts(api.NewCatalog(objects), cluster.Name()); !reflect.DeepEqual(got, facts) {
		t.Fatalf("catalog order changed facts: %+v / %+v", got, facts)
	}
}

func TestAddonFactsComposeNodeInterfacesAndRoutes(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	node, _ := catalog.Find(api.Machine, "node-0")
	network := node.Spec().Get("network")
	inline := network.Get("inline").With("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet"), m("name", "gone", "type", "ethernet", "state", "absent")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1"), m("destination", "::/0", "next-hop-interface", "eth1")))))
	node = node.WithSpec(node.Spec().With("network", network.With("inline", inline)))
	objects := []api.Object{cluster, node}
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Machine {
			objects = append(objects, o)
		}
	}
	catalog = api.NewCatalog(objects)
	cluster, _ = Normalize(cluster, catalog)
	objects[0] = cluster
	facts := AddonFacts(api.NewCatalog(objects), cluster.Name())
	got := facts.NodeNetworks[0]
	if got.DefaultRouteInterface != "eth0" || len(got.Interfaces) != 2 || got.Interfaces[0].Addresses[0] != netip.MustParsePrefix("192.0.2.10/24") || !slices.Contains(got.NextHops, netip.MustParseAddr("192.0.2.1")) {
		t.Fatalf("node facts: %+v", got)
	}
	if len(facts.Endpoints) != 3 {
		t.Fatalf("resolved node endpoints: %+v", facts.Endpoints)
	}
	if facts.Gateway.RoutingViaHost || facts.Gateway.IPForwarding != "" || len(facts.SecondaryNetworks) != 0 {
		t.Fatal("underived network settings")
	}
}

func TestAddonFactsCarryGatewayAndSecondaryNetworks(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	cluster, _ = Normalize(cluster, catalog)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("routingViaHost", true, "ipForwarding", "Global"), "networking", "ovnKubernetesConfig", "gatewayConfig").WithPath(list(m("name", "secondary", "bridgeMapping", m("interface", "eth1", "bridge", "br-storage"), "ipam", m("mode", "Enabled", "subnets", api.StringList("203.0.113.0/24")))), "networking", "secondaryNetworks"))
	facts := AddonFacts(api.NewCatalog(append(catalog.Objects(), cluster)), cluster.Name())
	if !facts.Gateway.RoutingViaHost || facts.Gateway.IPForwarding != "Global" || len(facts.SecondaryNetworks) != 1 {
		t.Fatalf("network settings: %+v", facts)
	}
	secondary := facts.SecondaryNetworks[0]
	if secondary.Name != "secondary" || secondary.Interface != "eth1" || secondary.Bridge != "br-storage" || !slices.Equal(secondary.Subnets, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}) {
		t.Fatalf("secondary: %+v", secondary)
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("Restricted"), "networking", "ovnKubernetesConfig", "gatewayConfig", "ipForwarding"))
	if facts := AddonFacts(api.NewCatalog(append(catalog.Objects(), cluster)), cluster.Name()); facts.Gateway.IPForwarding != "Restricted" {
		t.Fatal(facts.Gateway)
	}
}

func TestAddonFactsResolveEveryEndpointSource(t *testing.T) {
	for _, source := range []string{"node", "installer", "external", "loadBalancer"} {
		t.Run(source, func(t *testing.T) {
			count := 3
			if source == "node" {
				count = 1
			}
			cluster, catalog := fixture("libvirt", count, false)
			objects := catalog.Objects()
			if source != "node" {
				for _, slot := range []string{"api", "ingress"} {
					selected := m("type", source)
					if source == "loadBalancer" {
						selected = m("type", source, "loadBalancerRef", "endpoints", "bindAddressRef", slot)
					}
					if source == "loadBalancer" {
						cluster = cluster.WithSpec(cluster.Spec().WithPath(m("source", selected), "install", "endpoints", slot))
					} else {
						cluster = cluster.WithSpec(cluster.Spec().WithPath(selected, "install", "endpoints", slot, "source"))
					}
				}
			}
			if source == "loadBalancer" {
				objects = append(objects, obj(api.LoadBalancer, "endpoints", m("bindAddresses", list(m("name", "api", "address", "192.0.2.70"), m("name", "ingress", "address", "192.0.2.71")))))
			}
			cluster, issues := Normalize(cluster, api.NewCatalog(objects))
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			facts := AddonFacts(api.NewCatalog(append(objects, cluster)), cluster.Name())
			expected := []string{"192.0.2.2", "192.0.2.2", "192.0.2.3"}
			if source == "node" {
				expected = []string{"192.0.2.10", "192.0.2.10", "192.0.2.10"}
			}
			if source == "loadBalancer" {
				expected = []string{"192.0.2.70", "192.0.2.70", "192.0.2.71"}
			}
			if len(facts.Endpoints) != 3 {
				t.Fatal(facts.Endpoints)
			}
			for i, slot := range endpointSlots {
				if facts.Endpoints[i].Addr.String() != expected[i] || facts.Endpoints[i].Holder != "the "+slot+" endpoint" {
					t.Fatalf("%s: %+v", slot, facts.Endpoints)
				}
			}
		})
	}
}

func TestAddonFactsGatewayAndSecondaryNetworkTable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		gateway    api.Value
		networks   api.Value
		forwarding string
		routing    bool
		subnets    int
	}{
		{name: "absent"},
		{name: "host routing", gateway: m("routingViaHost", true), routing: true},
		{name: "global forwarding", gateway: m("routingViaHost", true, "ipForwarding", "Global"), routing: true, forwarding: "Global"},
		{name: "default bridge", networks: list(m("name", "external", "bridgeMapping", m("bridge", "br-ex"), "ipam", m("mode", "Disabled")))},
		{name: "dedicated bridge IPAM", networks: list(m("name", "external", "bridgeMapping", m("bridge", "br-secondary", "interface", "eth1"), "ipam", m("mode", "Enabled", "subnets", api.StringList("203.0.113.0/24")))), subnets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster, catalog := fixture("libvirt", 3, false)
			cluster, _ = Normalize(cluster, catalog)
			if tc.gateway.Present() {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(tc.gateway, "networking", "ovnKubernetesConfig", "gatewayConfig"))
			}
			if tc.networks.Present() {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(tc.networks, "networking", "secondaryNetworks"))
			}
			facts := AddonFacts(api.NewCatalog(append(catalog.Objects(), cluster)), cluster.Name())
			if facts.Gateway.RoutingViaHost != tc.routing || facts.Gateway.IPForwarding != tc.forwarding {
				t.Fatal(facts.Gateway)
			}
			if tc.networks.Len() != len(facts.SecondaryNetworks) {
				t.Fatal(facts.SecondaryNetworks)
			}
			if tc.networks.Len() > 0 {
				n := facts.SecondaryNetworks[0]
				input := tc.networks.Items()[0]
				if n.Name != "external" || n.Bridge != input.Get("bridgeMapping", "bridge").Text() || n.Interface != input.Get("bridgeMapping", "interface").Text() || len(n.Subnets) != tc.subnets {
					t.Fatal(n)
				}
			}
		})
	}
}

func TestMetalLBInterfacePoolUsesDerivedGatewayFacts(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	node, _ := catalog.Find(api.Machine, "node-0")
	network := node.Spec().Get("network")
	inline := network.Get("inline").With("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1")))))
	network = network.With("inline", inline).With("addresses", list(m("name", "primary", "interface", "eth0", "address", "192.0.2.10"), m("name", "secondary", "interface", "eth1", "address", "203.0.113.10")))
	node = node.WithSpec(node.Spec().With("network", network))
	objects := []api.Object{node, storageNetwork()}
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Machine {
			objects = append(objects, o)
		}
	}
	cluster, _ = Normalize(cluster, api.NewCatalog(objects))
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("routingViaHost", true, "ipForwarding", "Global"), "networking", "ovnKubernetesConfig", "gatewayConfig"))
	registry := addonpackages.Builtin()
	binding := obj(api.ClusterAddonBinding, "load-balancer", m("clusterRef", cluster.Name(), "packages", m("metallb", m("subscription", m("channel", "example-channel", "startingCSV", "metallb-operator.v0.0.0"), "pools", list(m("name", "services", "addresses", api.StringList("203.0.113.128-203.0.113.135"), "l2", m("interface", "eth1")))))))
	binding, _ = addons.NormalizePackages(binding, registry)
	nmstate := obj(api.ClusterAddonBinding, "node-network", m("clusterRef", cluster.Name(), "packages", m("nmstate", m("subscription", m("channel", "stable", "startingCSV", "kubernetes-nmstate-operator.v0.0.0")))))
	nmstate, _ = addons.NormalizePackages(nmstate, registry)
	for _, forwarding := range []string{"Global", "Restricted", ""} {
		gateway := cluster.Spec().WithPath(api.StringValue(forwarding), "networking", "ovnKubernetesConfig", "gatewayConfig", "ipForwarding")
		if forwarding == "" {
			gateway = cluster.Spec().WithPath(m("routingViaHost", true), "networking", "ovnKubernetesConfig", "gatewayConfig")
		}
		cluster := cluster.WithSpec(gateway)
		effective := api.NewCatalog(append(slices.Clone(objects), cluster, binding))
		issues := addons.ValidateWithRegistry(binding, effective, registry, AddonFacts(effective, cluster.Name()))
		if len(issues) != 1 || issues[0].Code != "api.unverified" || !strings.Contains(issues[0].Message, "interface eth1 of pool services is ethernet on node node-0, which keeps its kernel name") {
			t.Fatal(forwarding, issues)
		}
		forwarded := ForwardedInterfaces(cluster, effective, registry)
		providers := ValidateNetworkProviders(cluster, effective, registry)
		if forwarding == "Global" {
			if len(forwarded) != 0 || len(providers) != 0 {
				t.Fatalf("a globally forwarding cluster forwards no interface itself: %v %v", forwarded, providers)
			}
			continue
		}
		if !slices.Equal(forwarded, []string{"eth1"}) {
			t.Fatalf("%q: forwarded interfaces %v", forwarding, forwarded)
		}
		if len(providers) != 1 || providers[0].Field != "$.spec.networking.ovnKubernetesConfig.gatewayConfig" || !strings.Contains(providers[0].Message, "announce on eth1, where Bootwright enables IP forwarding through capability nmstate") || !strings.Contains(providers[0].Remediation, "ipForwarding: Global") {
			t.Fatalf("%q: nmstate requirement %+v", forwarding, providers)
		}
		if providers := ValidateNetworkProviders(cluster, api.NewCatalog(append(slices.Clone(objects), cluster, binding, nmstate)), registry); len(providers) != 0 {
			t.Fatalf("%q: an nmstate binding meets the requirement: %+v", forwarding, providers)
		}
	}
}

func TestAddonFactsClusterShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		nodes int
		ipv6  bool
	}{{"single", 1, false}, {"compact", 3, false}, {"IPv6 compact", 3, true}} {
		t.Run(tc.name, func(t *testing.T) {
			cluster, catalog := fixture("libvirt", tc.nodes, tc.ipv6)
			cluster, issues := Normalize(cluster, catalog)
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			facts := AddonFacts(api.NewCatalog(append(catalog.Objects(), cluster)), cluster.Name())
			want := "ipv4"
			if tc.ipv6 {
				want = "ipv6"
			}
			if facts.Family != want || len(facts.Nodes) != tc.nodes || len(facts.NodeNetworks) != tc.nodes || len(facts.MachineNetworks) != 1 || len(facts.ClusterNetworks) == 0 || len(facts.ServiceNetworks) == 0 {
				t.Fatalf("shape: %+v", facts)
			}
			for _, node := range facts.NodeNetworks {
				if len(node.Interfaces) != 1 || len(node.Interfaces[0].Addresses) != 1 || node.Interfaces[0].Addresses[0].Addr().Is4() == tc.ipv6 {
					t.Fatalf("node network: %+v", node)
				}
			}
		})
	}
}

func TestMetalLBPoolNamesTheBridgeThatCarriesTheUplinkAddresses(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	node, _ := catalog.Find(api.Machine, "node-0")
	network := node.Spec().Get("network")
	inline := network.Get("inline").With("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1")))))
	primary := m("name", "primary", "interface", "eth0", "address", "192.0.2.10")
	objectsWith := func(addresses ...api.Value) []api.Object {
		bound := node.WithSpec(node.Spec().With("network", network.With("inline", inline).With("addresses", list(addresses...))))
		objects := []api.Object{bound, storageNetwork()}
		for _, o := range catalog.Objects() {
			if o.Kind() != api.Machine {
				objects = append(objects, o)
			}
		}
		return objects
	}
	addressed := objectsWith(primary, m("name", "secondary", "interface", "eth1", "address", "203.0.113.10"))
	registry := addonpackages.Builtin()
	for _, test := range []struct {
		name          string
		hostAddresses string
		bridges       int
		iface         string
		message       string
		remedy        string
		objects       []api.Object
	}{
		{"bridge of a Bridge network", "bridge", 1, "br-storage", "", "", addressed},
		{"bridge shared by two Bridge networks", "bridge", 2, "br-storage", "", "", addressed},
		{"uplink of a Bridge network", "bridge", 1, "eth1", "is the bridge interface of secondary network storage0, whose bridge br-storage carries its node addresses", "set l2.interface to br-storage", addressed},
		{"bridge of a Bridge network over an addressless uplink", "bridge", 1, "br-storage", "interface br-storage of pool services carries no ipv4 address", "assign eth1 an address in the pool's subnet", objectsWith(primary)},
		{"bridge of a None network", "none", 1, "br-storage", "is the bridge interface of secondary network storage0, which carries no host address", "name an interface that carries a node address", addressed},
		{"uplink of a None network", "", 1, "eth1", "which carries no host address", "name an interface that carries a node address", addressed},
	} {
		objects := test.objects
		t.Run(test.name, func(t *testing.T) {
			networks := []api.Value{}
			for i := range test.bridges {
				uplink := m("interface", "eth1", "bridge", "br-storage")
				if test.hostAddresses != "" {
					uplink = uplink.With("hostAddresses", api.StringValue(test.hostAddresses))
				}
				networks = append(networks, m("name", fmt.Sprintf("storage%d", i), "bridgeMapping", uplink, "ipam", m("mode", "Disabled")))
			}
			cluster, _ := Normalize(cluster, api.NewCatalog(objects))
			cluster = cluster.WithSpec(cluster.Spec().WithPath(m("routingViaHost", true, "ipForwarding", "Global"), "networking", "ovnKubernetesConfig", "gatewayConfig").WithPath(list(networks...), "networking", "secondaryNetworks"))
			binding := obj(api.ClusterAddonBinding, "load-balancer", m("clusterRef", cluster.Name(), "packages", m("metallb", m("subscription", m("channel", "example-channel", "startingCSV", "metallb-operator.v0.0.0"), "pools", list(m("name", "services", "addresses", api.StringList("203.0.113.128-203.0.113.135"), "l2", m("interface", test.iface)))))))
			binding, _ = addons.NormalizePackages(binding, registry)
			effective := api.NewCatalog(append(slices.Clone(objects), cluster, binding))
			facts := AddonFacts(effective, cluster.Name())
			issues := addons.ValidateWithRegistry(binding, effective, registry, facts)
			if test.message == "" {
				if len(issues) > 0 {
					t.Fatal(issues)
				}
				bridges := 0
				for _, nic := range facts.NodeNetworks[0].Interfaces {
					if nic.Name == "br-storage" && nic.Type == "ovs-interface" && len(nic.Addresses) == 1 && nic.Addresses[0] == netip.MustParsePrefix("203.0.113.10/24") {
						bridges++
					}
					if nic.Name == "eth1" && len(nic.Addresses) != 0 {
						t.Fatalf("the uplink keeps its addresses: %+v", nic)
					}
				}
				if bridges != 1 {
					t.Fatalf("node facts carry %d bridge interfaces: %+v", bridges, facts.NodeNetworks[0].Interfaces)
				}
				return
			}
			if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
				return issue.Field == "$.spec.packages.metallb.pools[0].l2.interface" && strings.Contains(issue.Message, test.message) && strings.Contains(issue.Remediation, test.remedy)
			}) {
				t.Fatalf("refusal: %+v", issues)
			}
		})
	}
}

func TestMetalLBRefusesCephVirtualAddresses(t *testing.T) {
	for _, service := range []string{"ingress", "nfs", "mgmt-gateway"} {
		for _, raw := range []string{"192.0.2.130", "192.0.2.130/24"} {
			t.Run(service+"/"+raw, func(t *testing.T) {
				cluster, catalog := fixture("libvirt", 1, false)
				cluster, _ = Normalize(cluster, catalog)
				ceph := obj(api.CephCluster, "storage", m("services", list(m("service_type", service, "spec", m("virtual_ip", raw)))))
				binding := obj(api.ClusterAddonBinding, "load-balancer", m("clusterRef", cluster.Name(), "packages", m("metallb", m("subscription", m("channel", "stable", "startingCSV", "metallb.v1"), "pools", list(m("name", "services", "addresses", api.StringList("192.0.2.128-192.0.2.135")))))))
				registry := addonpackages.Builtin()
				binding, _ = addons.NormalizePackages(binding, registry)
				objects := append(catalog.Objects(), cluster, ceph, binding)
				effective := api.NewCatalog(objects)
				facts := AddonFacts(effective, cluster.Name())
				issues := addons.ValidateWithRegistry(binding, effective, registry, facts)
				if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
					return strings.Contains(issue.Message, "192.0.2.130, held by CephCluster/storage")
				}) {
					t.Fatalf("pool overlaps Ceph VIP without refusal: %+v", issues)
				}
				slices.Reverse(objects)
				if got := AddonFacts(api.NewCatalog(objects), cluster.Name()); !reflect.DeepEqual(got, facts) {
					t.Fatal("catalog order changed held addresses")
				}
			})
		}
	}
}

func withoutNetwork(catalog api.Catalog, name string) []api.Object {
	objects := []api.Object{}
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Network || o.Name() != name {
			objects = append(objects, o)
		}
	}
	return objects
}

func storageNetwork() api.Object {
	return obj(api.Network, "storage-lan", m("provided", true, "ipv4", m("cidr", "203.0.113.0/24")))
}
