package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func metalProvider(segments ...api.Value) api.Object {
	_, catalog := fixture()
	provider, _ := catalog.Find(api.InfraProvider, "metal")
	return provider.WithSpec(provider.Spec().With("segments", list(segments...)))
}

func twoNICConfig(interfaces ...api.Value) api.Object {
	return object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(interfaces...),
		"routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0"))))))
}

func twoNICMachine(eth1Segment string) api.Object {
	machine, _ := fixture()
	nics := list(m("nicRef", "eth0", "macAddress", "02:00:00:00:00:01", "segmentRef", "net"), m("nicRef", "eth1", "macAddress", "02:00:00:00:00:02"))
	if eth1Segment != "" {
		nics = list(nics.Items()[0], nics.Items()[1].With("segmentRef", api.StringValue(eth1Segment)))
	}
	machine = withNetwork(machine, []string{"substrate", "baremetal", "nics"}, nics)
	return withNetwork(machine, []string{"hardware", "nics"}, list(m("name", "eth0"), m("name", "eth1")))
}

func issueWith(issues []api.Issue, field, text string) (api.Issue, bool) {
	for _, issue := range issues {
		if issue.Field == field && strings.Contains(issue.Message, text) {
			return issue, true
		}
	}
	return api.Issue{}, false
}

// On a provider with two or more segments a bare-metal NIC an available
// ethernet interface matches names its segment once the graph declares a
// Network; an unmatched NIC may name none, and a NIC defaults to the sole
// segment of a provider that declares one.
func TestABareMetalNICOnASharedProviderNamesItsSegment(t *testing.T) {
	two := metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "baremetal", m()), m("name", "storage", "baremetal", m()))
	network := lan(m("ipv4", m("cidr", "192.0.2.0/24")))
	cabled := twoNICConfig(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet"))
	issue, found := issueWith(membershipIssues(t, twoNICMachine(""), two, network, cabled), "$.spec.substrate.baremetal.nics[1]", "NIC eth1 of Machine/node carries interface eth1")
	if !found || !strings.Contains(issue.Message, "declares segments net, storage") || !strings.Contains(issue.Remediation, "set segmentRef on the spec.substrate.baremetal.nics entry of NIC eth1 on Machine/node to one of net, storage") {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
	if issues := membershipIssues(t, twoNICMachine("storage"), two, network, cabled); len(issues) != 0 {
		t.Fatalf("a NIC naming its segment was refused: %+v", issues)
	}
	uncabled := twoNICConfig(m("name", "eth0", "type", "ethernet"))
	if issues := membershipIssues(t, twoNICMachine(""), two, network, uncabled); len(issues) != 0 {
		t.Fatalf("an unmatched NIC without a segment was refused: %+v", issues)
	}
	if _, found := issueWith(membershipIssues(t, twoNICMachine("absent-arm"), metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "baremetal", m()), m("name", "absent-arm", "libvirt", m("bridge", "br0"))), network, cabled),
		"$.spec.substrate.baremetal.nics[1].segmentRef", "segment must name a matching arm"); !found {
		t.Fatal("a segment of another arm was admitted")
	}
	bastion, _ := fixture()
	bastion = bastion.WithSpec(bastion.Spec().Without("substrate").WithPath(m("nics", list(m("nicRef", "eth0", "macAddress", "02:00:00:00:00:01", "segmentRef", "net"))), "substrate", "baremetal"))
	if _, found := issueWith(Validate(bastion, api.NewCatalog([]api.Object{bastion})), "$.spec.substrate.baremetal.nics[0].segmentRef", "names no provider whose segments it could select"); !found {
		t.Fatal("a segmentRef without a provider was admitted")
	}
}

// An interface's effective MTU, authored or inherited from its VLAN or bond
// parent, stays within the capacity of each Network it holds an address on.
func TestAnInterfaceStaysWithinTheMTUOfItsNetworks(t *testing.T) {
	machine, _ := fixture()
	jumbo := twoNICConfig(m("name", "eth0", "type", "ethernet", "mtu", api.IntegerValue("9000")))
	wide := metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "mtu", api.IntegerValue("9000"), "baremetal", m()))
	issue, found := issueWith(membershipIssues(t, machine, wide, jumbo, lan(m("ipv4", m("cidr", "192.0.2.0/24")))), "$.spec.network", "interface eth0 of Machine/node has MTU 9000 above capacity 1500 of Network/lan, on which it holds address 192.0.2.11")
	if !found || issue.Remediation != "lower the interface mtu in the network configuration of Machine/node, or raise spec.mtu of Network/lan" {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
	if issues := membershipIssues(t, machine, wide, jumbo, lan(m("mtu", api.IntegerValue("9000"), "ipv4", m("cidr", "192.0.2.0/24")))); len(issues) != 0 {
		t.Fatalf("an interface within its Network's capacity was refused: %+v", issues)
	}
	issue, found = issueWith(membershipIssues(t, machine, jumbo, lan(m("mtu", api.IntegerValue("9000"), "ipv4", m("cidr", "192.0.2.0/24")))), "$.spec.network", "above capacity 1500 of segment net (spec.segments[0] of InfraProvider/metal), which its NIC eth0 is on")
	if !found || !strings.HasSuffix(issue.Remediation, "or raise spec.segments[0].mtu of InfraProvider/metal") {
		t.Fatalf("a bare-metal segment did not bound its NIC: %+v (found %v)", issue, found)
	}
}

// A stack without a VLAN reaches its Network only where the leaf NIC's segment
// carries it untagged, a stack with one only where the segment carries it
// tagged, and every VLAN interface on a NIC tags a VLAN its segment carries.
func TestAnAssignmentsStackAgreesWithItsSegmentsCarriage(t *testing.T) {
	machine, _ := fixture()
	network := lan(m("ipv4", m("cidr", "192.0.2.0/24")))
	issue, found := issueWith(membershipIssues(t, machine, network, metalProvider(m("name", "net", "baremetal", m()))), "$.spec.network.addresses[0].interface", "interface eth0 of Machine/node reaches Network/lan untagged on NIC eth0, and segment net (spec.segments[0] of InfraProvider/metal) carries no Network untagged")
	if !found || issue.Remediation != "set spec.segments[0].untaggedNetworkRef of InfraProvider/metal to lan, or set segmentRef on the spec.substrate.baremetal.nics entry of NIC eth0 on Machine/node to a segment that carries it" {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
	other := object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	if _, found := issueWith(membershipIssues(t, machine, network, other, metalProvider(m("name", "net", "untaggedNetworkRef", "other", "baremetal", m()))), "$.spec.network.addresses[0].interface", "carries Network/other untagged"); !found {
		t.Fatal("a stack reaching a Network its segment carries untagged for another was admitted")
	}

	machine = withNetwork(machine, []string{"network", "addresses"}, list(m("name", "primary", "address", "192.0.2.11", "interface", "br0")))
	tagged := lan(m("vlan", api.IntegerValue("200"), "ipv4", m("cidr", "192.0.2.0/24")))
	issues := membershipIssues(t, machine, vlanConfig("200"), tagged, metalProvider(m("name", "net", "baremetal", m())))
	issue, found = issueWith(issues, "$.spec.network.addresses[0].interface", "interface br0 of Machine/node reaches Network/lan through a VLAN on NIC eth0, and segment net (spec.segments[0] of InfraProvider/metal) does not carry it tagged")
	if !found || !strings.HasPrefix(issue.Remediation, "add lan to spec.segments[0].taggedNetworkRefs of InfraProvider/metal") {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
	if issue, found := issueWith(issues, "$.spec.network.configRef", "VLAN interface eth0.200"); found {
		t.Fatalf("a VLAN whose assignment's carriage is refused is refused again: %+v", issue)
	}

	unassigned, _ := fixture()
	config := twoNICConfig(m("name", "eth0", "type", "ethernet"), m("name", "eth0.300", "type", "vlan", "vlan", m("base-iface", "eth0", "id", api.IntegerValue("300"))))
	if _, found := issueWith(membershipIssues(t, unassigned, network, config), "$.spec.network.configRef", "VLAN interface eth0.300 of Machine/node tags VLAN 300 on NIC eth0"); !found {
		t.Fatal("a VLAN without an assignment its segment does not carry was admitted")
	}
}

// A managed Network's segment carries the Network itself untagged, so a stack
// that reaches another Network untagged on it can only move.
func TestAManagedSegmentCarriesOnlyItsOwnNetworkUntagged(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	machine := guest(m("configRef", "net", "addresses", list(m("name", "ip", "address", "198.51.100.11", "interface", "enp1s0"))))
	other := object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	issue, found := issueWith(admission(machine, append(lab, other)...), "$.spec.network.addresses[0].interface", "reaches Network/other untagged on NIC enp1s0, and segment net (managed Network/net of InfraProvider/lab) carries Network/net untagged")
	if !found || issue.Remediation != "add an entry {nicRef: enp1s0, segmentRef: <segment>} to spec.substrate.libvirt.nics of Machine/guest naming a segment that carries Network/other untagged" {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
}

// An OVS internal interface has no ports of its own and is a port of its
// bridge, so its stack continues through the bridge's other ports to their
// NICs, untagged, since the subset admits no port VLAN settings.
func TestAnOVSInternalInterfaceReachesTheNICsOfItsBridge(t *testing.T) {
	ovs := func(mtu string) api.Object {
		internal := m("name", "ovs0", "type", "ovs-interface")
		if mtu != "" {
			internal = internal.With("mtu", api.IntegerValue(mtu))
		}
		return object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(
			m("name", "eth0", "type", "ethernet"),
			m("name", "ovsbr", "type", "ovs-bridge", "bridge", m("port", list(m("name", "eth0"), m("name", "ovs0")))),
			internal),
			"routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "ovs0"))))))
	}
	machine, _ := fixture()
	machine = withNetwork(machine, []string{"network", "addresses"}, list(m("name", "primary", "address", "192.0.2.11", "interface", "ovs0")))
	tagged := lan(m("vlan", api.IntegerValue("200"), "ipv4", m("cidr", "192.0.2.0/24")))
	other := object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	carrying := metalProvider(m("name", "net", "untaggedNetworkRef", "other", "baremetal", m()))
	if _, found := issueWith(membershipIssues(t, machine, ovs(""), tagged, other, carrying), "$.spec.network.addresses[0].interface",
		"interface ovs0 of Machine/node reaches Network/lan untagged on NIC eth0, and segment net (spec.segments[0] of InfraProvider/metal) carries Network/other untagged"); !found {
		t.Fatalf("an OVS internal interface was not followed to its bridge's NIC: %+v", membershipIssues(t, machine, ovs(""), tagged, other, carrying))
	}
	wide := lan(m("mtu", api.IntegerValue("9000"), "ipv4", m("cidr", "192.0.2.0/24")))
	if _, found := issueWith(membershipIssues(t, machine, ovs("9000"), wide), "$.spec.network", "interface ovs0 of Machine/node has MTU 9000 above capacity 1500 of segment net (spec.segments[0] of InfraProvider/metal), which its NIC eth0 is on"); !found {
		t.Fatalf("an OVS internal interface was not bounded by its bridge's segment: %+v", membershipIssues(t, machine, ovs("9000"), wide))
	}
}

// A bridge whose ports sit on two segments reaches its Network through each of
// them, so each segment must carry it; a bond across segments stays the bond
// rule's refusal.
func TestABridgeAcrossSegmentsNeedsEachToCarryItsNetwork(t *testing.T) {
	two := metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "baremetal", m()), m("name", "storage", "untaggedNetworkRef", "other", "baremetal", m()))
	other := object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	config := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(
		m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet"),
		m("name", "br0", "type", "linux-bridge", "bridge", m("port", list(m("name", "eth0"), m("name", "eth1")))))),
		"routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "br0")))))
	machine := withNetwork(twoNICMachine("storage"), []string{"network", "addresses"}, list(m("name", "primary", "address", "192.0.2.11", "interface", "br0")))
	issues := membershipIssues(t, machine, two, other, config, lan(m("ipv4", m("cidr", "192.0.2.0/24"))))
	issue, found := issueWith(issues, "$.spec.network.addresses[0].interface", "interface br0 of Machine/node reaches Network/lan untagged on NIC eth1, and segment storage (spec.segments[1] of InfraProvider/metal) carries Network/other untagged")
	if !found || issue.Remediation != "set spec.segments[1].untaggedNetworkRef of InfraProvider/metal to lan, or move NIC eth1 with its segmentRef on Machine/node to a segment that carries it" {
		t.Fatalf("issue = %+v (found %v) in %+v", issue, found, issues)
	}
	if _, found := issueWith(issues, "$.spec.network.addresses[0].interface", "on NIC eth0"); found {
		t.Fatalf("the segment that carries the Network was refused: %+v", issues)
	}
}

// A KubeVirt segment carries one network untagged and refuses tagged carriage,
// so a VLAN stack on it is told to drop the VLAN or move the NIC, never to add
// a tagged Network.
func TestAFabricSegmentNeverOffersTaggedCarriage(t *testing.T) {
	provider := object(api.InfraProvider, "kv", m("kubevirt", m("kubeconfigRef", "host", "namespace", "guests"), "segments", list(
		m("name", "primary", "untaggedNetworkRef", "other", "kubevirt", m()), m("name", "storage", "untaggedNetworkRef", "lan", "kubevirt", m()))))
	machine := object(api.Machine, "node", m("substrate", m("providerRef", "kv", "kubevirt", m("nics", list(m("nicRef", "eth0", "segmentRef", "primary")))), "hardware", m("nics", list(m("name", "eth0")))))
	native := m("interfaces", list(m("name", "eth0", "type", "ethernet"), m("name", "eth0.200", "type", "vlan", "vlan", m("base-iface", "eth0", "id", api.IntegerValue("200")))))
	tagged := lan(m("vlan", api.IntegerValue("200"), "ipv4", m("cidr", "192.0.2.0/24")))
	issues := validateCarriage(machine, api.NewCatalog([]api.Object{machine, provider, tagged}), native, tagged, "eth0.200", "$.spec.network.addresses[0].interface")
	want := "remove VLAN interface eth0.200 from the stack of interface eth0.200 on Machine/node, or move NIC eth0 with its segmentRef on Machine/node to a segment that has untaggedNetworkRef lan"
	if len(issues) != 1 || issues[0].Remediation != want {
		t.Fatalf("issues = %+v", issues)
	}
}

// An interface without an mtu of its own runs at 1500, so above a smaller
// capacity the step is to set one, and a bare-metal NIC without a nics entry is
// told the entry it needs, macAddress included.
func TestRemediesNameTheFieldsTheOperatorAdds(t *testing.T) {
	machine, _ := fixture()
	narrow := metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "mtu", api.IntegerValue("1400"), "baremetal", m()))
	issue, found := issueWith(membershipIssues(t, machine, narrow, lan(m("ipv4", m("cidr", "192.0.2.0/24")))), "$.spec.network", "interface eth0 of Machine/node has MTU 1500 above capacity 1400")
	if !found || issue.Remediation != "set mtu on interface eth0 to at most 1400 in the network configuration of Machine/node, or raise spec.segments[0].mtu of InfraProvider/metal" {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
	two := metalProvider(m("name", "net", "untaggedNetworkRef", "lan", "baremetal", m()), m("name", "storage", "baremetal", m()))
	cabled := twoNICConfig(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet"))
	unlisted := twoNICMachine("")
	unlisted = withNetwork(unlisted, []string{"substrate", "baremetal", "nics"}, list(unlisted.Spec().Get("substrate", "baremetal", "nics").Items()[0]))
	issue, found = issueWith(membershipIssues(t, unlisted, two, lan(m("ipv4", m("cidr", "192.0.2.0/24"))), cabled), "$.spec.substrate.baremetal.nics", "NIC eth1 of Machine/node carries interface eth1")
	if !found || issue.Remediation != "add an entry {nicRef: eth1, macAddress: <its MAC>, segmentRef: <segment>} to spec.substrate.baremetal.nics of Machine/node, with segmentRef one of net, storage" {
		t.Fatalf("issue = %+v (found %v)", issue, found)
	}
}
