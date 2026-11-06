package machine

import (
	"slices"
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

// A provider's sole attachment is the default whatever its name, for a
// referenced or an inline configuration; the NetworkConfig's name selects
// nothing, and two attachments still need an explicit selection.
func TestAMachineDefaultsToItsProvidersSoleAttachment(t *testing.T) {
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
			if got := normalized.Spec().Get("network", "attachmentRef").Text(); got != "net" {
				t.Fatalf("attachmentRef = %q, want the sole attachment net", got)
			}
			if issues := admission(machine, lab...); len(issues) != 0 {
				t.Fatalf("a Machine on its provider's sole attachment was refused: %v", issues)
			}
			selected := machine.WithSpec(machine.Spec().WithPath(list(m("interface", "enp1s0", "attachmentRef", "net")), "substrate", "libvirt", "interfaceAttachments"))
			if normalized, _ := Normalize(selected, catalog); normalized.Spec().Has("network", "attachmentRef") {
				t.Fatal("a default was added beside authored interfaceAttachments")
			}
		})
	}
	machine := guest(m("configRef", "net", "addresses", addresses))
	requireOneIssue(t, admission(machine, twoAttachmentLab(list(m("name", "enp1s0", "type", "ethernet")))...), api.Issue{Code: "api.invariant", Field: "$.spec.network.attachmentRef",
		Message:     "provider-backed configured networks require an explicit attachment unless their provider has exactly one",
		Remediation: "set spec.network.attachmentRef on Machine/guest to an attachment of InfraProvider/lab"})
}

// A bond's ports share one attachment: ports on two would move the host onto
// another network at the first failover.
func TestABondRefusesPortsOnDifferentAttachments(t *testing.T) {
	interfaces := list(m("name", "enp1s0", "type", "ethernet"), m("name", "enp2s0", "type", "ethernet"),
		m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("enp1s0", "enp2s0"))))
	bonded := func(second string) api.Object {
		machine := guest(m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "bond0"))))
		return machine.WithSpec(machine.Spec().WithPath(list(m("interface", "enp1s0", "attachmentRef", "net"), m("interface", "enp2s0", "attachmentRef", second)), "substrate", "libvirt", "interfaceAttachments"))
	}
	requireOneIssue(t, admission(bonded("storage"), twoAttachmentLab(interfaces)...), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.interfaceAttachments[1].attachmentRef",
		Message:     "bond bond0 of Machine/guest has ports on different attachments (enp1s0 on net, enp2s0 on storage), so a failover would move the host onto another network",
		Remediation: "attach every port of bond bond0 to one attachment in spec.substrate.libvirt.interfaceAttachments on Machine/guest, or remove the port on another attachment from the bond"})
	if issues := admission(bonded("net"), twoAttachmentLab(interfaces)...); len(issues) != 0 {
		t.Fatalf("a bond on one attachment was refused: %v", issues)
	}
}

// The install gateway is the next hop of the install interface's default
// route, so it lies inside the install address's prefix; a Machine with no
// such route, or a route on another interface, is untouched.
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
