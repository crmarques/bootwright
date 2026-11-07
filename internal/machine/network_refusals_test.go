package machine

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// An OS-ready Machine is told once to remove a network selection, never also
// to select a configuration it may not declare.
func TestAnOSReadyMachineIsToldOnlyToRemoveASelection(t *testing.T) {
	ready := object(api.Machine, "ready", m("os", m("provided", true), "network", m("installAddressRef", "ip")))
	var found []api.Issue
	for _, issue := range Validate(ready, api.NewCatalog([]api.Object{ready})) {
		if issue.Field == "$.spec.network.installAddressRef" {
			found = append(found, issue)
		}
	}
	if len(found) != 1 || found[0].Message != "OS-ready Machines declare contacts only" {
		t.Fatalf("issues = %+v", found)
	}
}

// Nothing installs an OS-ready Machine, so a root-device hint only a disk
// selector reads warns api.deferred, while deviceName and wwn, which keep the
// root disk out of Ceph OSD selections, do not.
func TestAnOSReadyMachineWarnsOfRootHintsNothingReads(t *testing.T) {
	ready := object(api.Machine, "ready", m("os", m("provided", true),
		"hardware", m("rootDevice", m("deviceName", "/dev/vda", "model", "disk", "vendor", "acme", "minSizeGigabytes", api.IntegerValue("10"), "rotational", false)),
		"substrate", m("baremetal", m("rootDevice", m("wwn", "0x5000c500a1b2c3d4", "hctl", "0:0:0:0", "serialNumber", "s1")))))
	deferred := []string{}
	for _, issue := range Validate(ready, api.NewCatalog([]api.Object{ready})) {
		if issue.Code == "api.deferred" {
			deferred = append(deferred, issue.Field)
		}
	}
	want := []string{"$.spec.substrate.baremetal.rootDevice.hctl", "$.spec.hardware.rootDevice.minSizeGigabytes", "$.spec.hardware.rootDevice.model", "$.spec.hardware.rootDevice.rotational", "$.spec.substrate.baremetal.rootDevice.serialNumber", "$.spec.hardware.rootDevice.vendor"}
	if !slices.Equal(deferred, want) {
		t.Fatalf("deferred = %v, want %v", deferred, want)
	}
}

// twoAttachmentLab is the lab with a second, external attachment storage.
func twoAttachmentLab(interfaces api.Value) []api.Object {
	objects := libvirtLab(interfaces)
	for i, o := range objects {
		if o.Kind() == api.InfraProvider {
			attachments := append(o.Spec().Get("networkAttachments").Items(), m("name", "storage", "libvirt", m("bridge", "br-storage")))
			objects[i] = o.WithSpec(o.Spec().With("networkAttachments", list(attachments...)))
		}
	}
	return objects
}

// A provider's sole attachment is every NIC's default whatever its name, for a
// referenced or an inline configuration; the NetworkConfig's name selects
// nothing, and with two attachments every NIC names its own.
func TestEveryNICDefaultsToItsProvidersSoleAttachment(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	for i, o := range lab {
		if o.Kind() == api.NetworkConfig {
			lab[i] = api.NewObject(api.NetworkConfig, "profile", api.MapValue(), o.Spec())
		}
	}
	addresses := list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0"))
	inline := m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet"))))
	for name, network := range map[string]api.Value{
		"a referenced configuration": m("configRef", "profile", "addresses", addresses),
		"an inline configuration":    m("inline", inline, "addresses", addresses),
	} {
		t.Run(name, func(t *testing.T) {
			machine := guest(network)
			catalog := api.NewCatalog(append([]api.Object{machine}, lab...))
			normalized, _ := Normalize(machine, catalog)
			if got := normalized.Spec().Get("substrate", "libvirt", "nics"); !got.Equal(list(m("nicRef", "enp1s0", "attachmentRef", "net"))) {
				t.Fatalf("substrate.libvirt.nics = %v, want enp1s0 on the sole attachment net", got)
			}
			if issues := admission(machine, lab...); len(issues) != 0 {
				t.Fatalf("a Machine on its provider's sole attachment was refused: %v", issues)
			}
		})
	}
	machine := guest(m("configRef", "net", "addresses", addresses))
	requireOneIssue(t, admission(machine, twoAttachmentLab(list(m("name", "enp1s0", "type", "ethernet")))...), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.nics",
		Message:     "NIC enp1s0 of Machine/guest selects no network attachment, and InfraProvider/lab declares 2 attachments, not exactly one that every NIC defaults to",
		Remediation: "add a spec.substrate.libvirt.nics entry with nicRef enp1s0 and an attachmentRef naming an attachment of InfraProvider/lab on Machine/guest"})
	selected := machine.WithSpec(machine.Spec().WithPath(list(m("nicRef", "enp1s0", "attachmentRef", "storage")), "substrate", "libvirt", "nics"))
	if issues := admission(selected, twoAttachmentLab(list(m("name", "enp1s0", "type", "ethernet")))...); len(issues) != 0 {
		t.Fatalf("a NIC naming one of two attachments was refused: %v", issues)
	}
}

// A bond's ports share one attachment: ports on two would move the host onto
// another network at the first failover.
func TestABondRefusesPortsOnDifferentAttachments(t *testing.T) {
	interfaces := list(m("name", "enp1s0", "type", "ethernet"), m("name", "enp2s0", "type", "ethernet"),
		m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("enp1s0", "enp2s0"))))
	bonded := func(second string) api.Object {
		machine := guest(m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "bond0"))))
		return machine.WithSpec(machine.Spec().WithPath(list(m("nicRef", "enp1s0", "attachmentRef", "net"), m("nicRef", "enp2s0", "attachmentRef", second)), "substrate", "libvirt", "nics").
			WithPath(list(m("name", "enp1s0"), m("name", "enp2s0")), "hardware", "nics"))
	}
	requireOneIssue(t, admission(bonded("storage"), twoAttachmentLab(interfaces)...), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.nics[1].attachmentRef",
		Message:     "bond bond0 of Machine/guest has ports on different attachments (enp1s0 on net, enp2s0 on storage), so a failover would move the host onto another network",
		Remediation: "attach the NIC of every port of bond bond0 to one attachment in spec.substrate.libvirt.nics on Machine/guest, or remove the port on another attachment from the bond"})
	if issues := admission(bonded("net"), twoAttachmentLab(interfaces)...); len(issues) != 0 {
		t.Fatalf("a bond on one attachment was refused: %v", issues)
	}
}

// A bond's ports include those an ethernet interface names it the controller
// of, and a split bond nested in another is reported once, at the inner bond.
func TestABondSplitThroughControllerOrNestingRefusesOnce(t *testing.T) {
	ethernet := func(name, controller string) api.Value {
		if controller == "" {
			return m("name", name, "type", "ethernet")
		}
		return m("name", name, "type", "ethernet", "controller", controller)
	}
	attached := func(interfaces api.Value, install string, attachments ...string) []api.Issue {
		machine := guest(m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", install))))
		var entries, nics []api.Value
		for index, reference := range attachments {
			name := []string{"enp1s0", "enp2s0", "enp3s0"}[index]
			entries, nics = append(entries, m("nicRef", name, "attachmentRef", reference)), append(nics, m("name", name))
		}
		machine = machine.WithSpec(machine.Spec().WithPath(list(entries...), "substrate", "libvirt", "nics").WithPath(list(nics...), "hardware", "nics"))
		return admission(machine, twoAttachmentLab(interfaces)...)
	}
	controlled := list(ethernet("enp1s0", "bond0"), ethernet("enp2s0", "bond0"), m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup")))
	requireOneIssue(t, attached(controlled, "bond0", "net", "storage"), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.nics[1].attachmentRef",
		Message:     "bond bond0 of Machine/guest has ports on different attachments (enp1s0 on net, enp2s0 on storage), so a failover would move the host onto another network",
		Remediation: "attach the NIC of every port of bond bond0 to one attachment in spec.substrate.libvirt.nics on Machine/guest, or remove the port on another attachment from the bond"})
	if issues := attached(controlled, "bond0", "net", "net"); len(issues) != 0 {
		t.Fatalf("a controller-declared bond on one attachment was refused: %v", issues)
	}
	nested := list(ethernet("enp1s0", ""), ethernet("enp2s0", ""), ethernet("enp3s0", ""),
		m("name", "bond1", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("enp1s0", "enp2s0"))),
		m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("bond1", "enp3s0"))))
	var refused []string
	for _, issue := range attached(nested, "bond0", "net", "storage", "net") {
		if strings.Contains(issue.Message, "has ports on different attachments") {
			refused = append(refused, issue.Message)
		}
	}
	if len(refused) != 1 || !strings.HasPrefix(refused[0], "bond bond1 of Machine/guest") {
		t.Fatalf("a split nested bond was reported %d times: %v", len(refused), refused)
	}
}

// The install gateway is the next hop of the install interface's default
// route, so it lies inside the install address's prefix; a Machine with no
// such route, a route on another interface, or an IPv6 link-local next hop,
// which is on-link by definition, is untouched.
func TestAnInstallGatewayLiesInsideTheInstallPrefix(t *testing.T) {
	node, catalog := fixture()
	routed := func(route api.Value) []api.Issue {
		objects := without(catalog, node)
		for i, o := range objects {
			if o.Kind() == api.NetworkConfig {
				objects[i] = o.WithSpec(o.Spec().WithPath(list(route), "nmstate", "routes", "config"))
			}
		}
		return admission(node, objects...)
	}
	outside := api.Issue{Code: "api.invariant", Field: "$.spec.network.installAddressRef",
		Message:     "install gateway 198.51.100.1, the next hop of the default route in NetworkConfig/net, lies outside install address primary 192.0.2.11/24 of Machine/node",
		Remediation: "set that default route's next-hop-address in NetworkConfig/net to a gateway inside 192.0.2.0/24, or give install address primary a prefix that contains 198.51.100.1"}
	requireOneIssue(t, routed(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "198.51.100.1")), outside)
	requireOneIssue(t, routed(m("destination", "0.0.0.0/0", "next-hop-address", "198.51.100.1")), outside)
	for name, route := range map[string]api.Value{
		"inside the prefix":    m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1"),
		"on another interface": m("destination", "0.0.0.0/0", "next-hop-interface", "eth1", "next-hop-address", "198.51.100.1"),
		"an absent route":      m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "198.51.100.1", "state", "absent"),
		"no default route":     m("destination", "198.51.100.0/24", "next-hop-interface", "eth0", "next-hop-address", "198.51.100.1"),
	} {
		if issues := routed(route); len(issues) != 0 {
			t.Fatalf("%s was refused: %v", name, issues)
		}
	}
	v6 := node.WithSpec(node.Spec().WithPath(list(m("name", "primary", "address", "2001:db8::11/64", "interface", "eth0")), "network", "addresses"))
	routedV6 := func(gateway string) []api.Issue {
		objects := without(catalog, node)
		for i, o := range objects {
			if o.Kind() == api.NetworkConfig {
				objects[i] = o.WithSpec(o.Spec().With("machineNetwork", list(m("cidr", "2001:db8::/64"))).WithPath(list(m("destination", "::/0", "next-hop-interface", "eth0", "next-hop-address", gateway)), "nmstate", "routes", "config"))
			}
		}
		return admission(v6, objects...)
	}
	if issues := routedV6("fe80::1"); len(issues) != 0 {
		t.Fatalf("an IPv6 link-local next hop, on-link by definition, was refused: %v", issues)
	}
	if issues := routedV6("2001:db8:1::1"); len(issues) != 1 || issues[0].Message != "install gateway 2001:db8:1::1, the next hop of the default route in NetworkConfig/net, lies outside install address primary 2001:db8::11/64 of Machine/node" {
		t.Fatalf("an IPv6 gateway outside the prefix: %#v", issues)
	}
	inline := node.WithSpec(node.Spec().With("network", node.Spec().Get("network").Without("configRef").With("inline", m("machineNetwork", list(m("cidr", "192.0.2.0/24")),
		"nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-address", "198.51.100.1"))))))))
	issues := admission(inline, without(catalog, node)...)
	if len(issues) != 1 || issues[0].Message != "install gateway 198.51.100.1, the next hop of the default route in spec.network.inline.nmstate, lies outside install address primary 192.0.2.11/24 of Machine/node" {
		t.Fatalf("an inline network's gateway: %#v", issues)
	}
}

// A contact names a host to reach and configures nothing, so it carries no
// prefix.
func TestAContactCarriesNoPrefix(t *testing.T) {
	ready := func(address string) []api.Issue {
		machine := object(api.Machine, "ready", m("os", m("provided", true), "network", m("addresses", list(m("name", "ssh", "address", address)))))
		return admission(machine)
	}
	requireOneIssue(t, ready("192.0.2.10/24"), api.Issue{Code: "api.invariant", Field: "$.spec.network.addresses[0].address",
		Message:     "contact ssh of Machine/ready carries a prefix, but a contact names a host to reach and configures nothing",
		Remediation: "remove the prefix from spec.network.addresses[0].address on Machine/ready, or assign the address to an interface with spec.network.addresses[0].interface"})
	for _, address := range []string{"192.0.2.10", "2001:db8::10", "host.example.test"} {
		if issues := ready(address); len(issues) != 0 {
			t.Fatalf("contact %s was refused: %v", address, issues)
		}
	}
}

// A NIC that matches no interface but is named as an interface another NIC
// carries would put that interface name in the installer's host list twice,
// with two addresses.
func TestAnUnmatchedNICMayNotShareAMatchedInterfaceName(t *testing.T) {
	node, catalog := fixture()
	labelled := func(second string) api.Object {
		return node.WithSpec(node.Spec().
			WithPath(list(m("name", "nic-a"), m("name", second)), "hardware", "nics").
			WithPath(list(m("nicRef", "nic-a", "macAddress", "02:00:00:00:00:01"), m("nicRef", second, "macAddress", "02:00:00:00:00:02")), "substrate", "baremetal", "nics").
			WithPath(list(m("interface", "eth0", "nicRef", "nic-a")), "network", "interfaceMatches"))
	}
	requireOneIssue(t, admission(labelled("eth0"), without(catalog, node)...), api.Issue{Code: "api.invariant", Field: "$.spec.network.interfaceMatches[0].nicRef",
		Message:     "hardware NIC eth0 of Machine/node matches no interface but shares the name of interface eth0, which NIC nic-a carries, so the installer would list eth0 twice with two addresses",
		Remediation: "match NIC eth0 to an interface in spec.network.interfaceMatches on Machine/node, or rename it in spec.hardware.nics"})
	if issues := admission(labelled("nic-b"), without(catalog, node)...); len(issues) != 0 {
		t.Fatalf("an unmatched NIC with its own name was refused: %v", issues)
	}
}

// Every available ethernet interface matches one NIC: an omitted match is the
// NIC of the interface's name, and two interfaces never share one NIC. A NIC no
// interface names stays unconfigured.
func TestEveryEthernetInterfaceMatchesExactlyOneNIC(t *testing.T) {
	two := list(m("name", "enp1s0", "type", "ethernet"), m("name", "enp2s0", "type", "ethernet"))
	matched := func(nics api.Value, matches api.Value) []api.Issue {
		machine := guest(m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0"))))
		spec := machine.Spec().WithPath(nics, "hardware", "nics")
		if matches.Present() {
			spec = spec.WithPath(matches, "network", "interfaceMatches")
		}
		return admission(machine.WithSpec(spec), libvirtLab(two)...)
	}
	requireOneIssue(t, matched(list(m("name", "enp1s0")), api.Value{}), api.Issue{Code: "api.invariant", Field: "$.spec.network.interfaceMatches",
		Message:     "ethernet interface enp2s0 of Machine/guest matches no NIC: spec.hardware.nics declares no NIC enp2s0 and spec.network.interfaceMatches does not match it",
		Remediation: "declare NIC enp2s0 in spec.hardware.nics on Machine/guest, or match interface enp2s0 to a NIC in spec.network.interfaceMatches"})
	requireOneIssue(t, matched(list(m("name", "enp1s0"), m("name", "enp2s0")), list(m("interface", "enp2s0", "nicRef", "enp1s0"))), api.Issue{Code: "api.invariant", Field: "$.spec.network.interfaceMatches",
		Message:     "NIC enp1s0 of Machine/guest is matched by interface enp1s0 and by interface enp2s0, and one NIC carries one ethernet interface",
		Remediation: "match one of the two interfaces to another NIC in spec.network.interfaceMatches on Machine/guest"})
	requireOneIssue(t, matched(list(m("name", "enp1s0"), m("name", "enp2s0")), list(m("interface", "eth9", "nicRef", "enp1s0"))), api.Issue{Code: "api.reference", Field: "$.spec.network.interfaceMatches[0].interface",
		Message:     "interface eth9 of Machine/guest is no available ethernet interface of its composed network configuration, so it matches no NIC",
		Remediation: "set spec.network.interfaceMatches[0].interface to an available ethernet interface of the network configuration, or remove the entry"})
	if issues := matched(list(m("name", "enp1s0"), m("name", "enp2s0"), m("name", "spare")), api.Value{}); len(issues) != 0 {
		t.Fatalf("a NIC no interface names was refused: %v", issues)
	}
	if issues := matched(list(m("name", "vnic0"), m("name", "vnic1")), list(m("interface", "enp1s0", "nicRef", "vnic0"), m("interface", "enp2s0", "nicRef", "vnic1"))); len(issues) != 0 {
		t.Fatalf("interfaces matched to differently named vNICs were refused: %v", issues)
	}
}

// A libvirt or KubeVirt provider derives each vNIC's MAC, so a Machine it
// realizes authors none in its NMState.
func TestAVirtualMachineAuthorsNoNMStateMAC(t *testing.T) {
	interfaces := list(m("name", "enp1s0", "type", "ethernet", "mac-address", "52:54:00:00:00:01"))
	machine := guest(m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0"))))
	requireOneIssue(t, admission(machine, libvirtLab(interfaces)...), api.Issue{Code: "api.invariant", Field: "$.spec.network.configRef",
		Message:     "ethernet interface enp1s0 of Machine/guest authors a mac-address, but a libvirt provider derives each vNIC's MAC from the context, Machine and NIC names",
		Remediation: "remove mac-address from interface enp1s0 in the network configuration of Machine/guest, and match the interface to its NIC with spec.network.interfaceMatches if their names differ"})
}

// A libvirt or KubeVirt installer pins every vNIC, an unmatched one under its
// NIC name, so that name may not be a matched interface's.
func TestAnUnmatchedVirtualNICMayNotTakeAPinnedInterfaceName(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	nics := func(second string) api.Object {
		machine := installOn("192.0.2.11/24")
		spec := machine.Spec().WithPath(list(m("name", "vnic0"), m("name", second)), "hardware", "nics").
			WithPath(list(m("interface", "enp1s0", "nicRef", "vnic0")), "network", "interfaceMatches")
		return machine.WithSpec(spec)
	}
	requireOneIssue(t, admission(nics("enp1s0"), lab...), api.Issue{Code: "api.invariant", Field: "$.spec.network.interfaceMatches[0].nicRef",
		Message:     "hardware NIC enp1s0 of Machine/guest matches no interface but shares the name of interface enp1s0, which NIC vnic0 carries, so the installer would list enp1s0 twice with two addresses",
		Remediation: "match NIC enp1s0 to an interface in spec.network.interfaceMatches on Machine/guest, or rename it in spec.hardware.nics"})
	if issues := admission(nics("spare"), lab...); len(issues) != 0 {
		t.Fatalf("an unmatched vNIC with its own name was refused: %v", issues)
	}
}
