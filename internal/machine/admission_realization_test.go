package machine

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

// libvirtLab is a libvirt provider on its host Machine, the managed Network it
// defines with host address 192.0.2.1/24, a network configuration presenting
// interfaces, an Anaconda install profile and an Environment domain.
func libvirtLab(interfaces api.Value) []api.Object {
	return []api.Object{
		object(api.Machine, "host", m("os", m("provided", true))),
		object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulation", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small"))))),
		object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", interfaces))),
		object(api.MachineInstallProfile, "rhel", m("installer", m("anaconda", m("imageRef", "image")))),
		object(api.Environment, "env", m("domains", m("base", "example.test"))),
		managedNet("net", "lab", "192.0.2.0/24", "192.0.2.1"),
	}
}

// guest is an installer-provisioned Machine of the lab provider with network.
func managedNet(name, provider, cidr, host string) api.Object {
	return object(api.Network, name, m("provided", false, "mtu", api.IntegerValue("1500"), "ipv4", m("cidr", cidr),
		"libvirt", m("providerRef", provider, "bridge", "virbr-lab", "forward", "nat", "hostAddress", host)))
}

func guest(network api.Value) api.Object {
	spec := m("substrate", m("providerRef", "lab", "libvirt", m("profileRef", "small")), "os", m("provided", false), "hardware", m("nics", list(m("name", "enp1s0"))))
	if network.Present() {
		spec = spec.With("network", network)
	}
	return object(api.Machine, "guest", spec)
}

// admission is what the compiler asks of one Machine: its authored rules, then
// its effective rules once normalized beside the other objects.
func admission(machine api.Object, others ...api.Object) []api.Issue {
	objects := append([]api.Object{machine}, others...)
	issues := ValidateAuthored(machine, api.NewCatalog(objects))
	normalized, _ := Normalize(machine, api.NewCatalog(objects))
	objects[0] = normalized
	return append(issues, Validate(normalized, api.NewCatalog(objects))...)
}

func requireOneIssue(t *testing.T, issues []api.Issue, want api.Issue) {
	t.Helper()
	if len(issues) != 1 || issues[0] != want {
		t.Fatalf("issues = %#v, want only %#v", issues, want)
	}
}

// A Machine a libvirt provider realizes is reached through the ethernet
// interfaces its configuration selects, so a non-provided Machine with none
// refuses at admission rather than at plan, after composition and its
// overrides. An installed Anaconda Machine without a network keeps the one
// refusal its installation rule already gives.
func TestALibvirtMachineNeedsAnAvailableEthernetInterface(t *testing.T) {
	up := list(m("name", "enp1s0", "type", "ethernet", "state", "up"))
	absent := list(m("name", "enp1s0", "type", "ethernet", "state", "absent"))
	attached := m("configRef", "net")
	unavailable := api.Issue{Code: "api.invariant", Field: "$.spec.network.configRef",
		Message:     "the network configuration presents no available ethernet interface, so no NIC of the Machine would be configured",
		Remediation: "declare an ethernet interface that is not absent or ignored in the NMState Machine/guest selects, or select another configuration"}
	t.Run("no network", func(t *testing.T) {
		requireOneIssue(t, admission(guest(api.Value{}), libvirtLab(up)...), api.Issue{Code: "api.invariant", Field: "$.spec.network",
			Message:     "a Machine a libvirt provider realizes is reached through the network configuration its operating system applies, and this Machine selects none",
			Remediation: "select a network configuration on Machine/guest with spec.network.configRef or spec.network.inline, with an ethernet interface that is not absent or ignored"})
	})
	for name, test := range map[string]struct {
		interfaces, network api.Value
		field               string
	}{
		"only an absent ethernet interface":   {absent, attached, "$.spec.network.configRef"},
		"only an ignored ethernet interface":  {list(m("name", "enp1s0", "type", "ethernet", "state", "ignore")), attached, "$.spec.network.configRef"},
		"only a bond":                         {list(m("name", "bond0", "type", "bond")), attached, "$.spec.network.configRef"},
		"an override removing the interface":  {up, attached.With("overrides", m("interfaces", list(m("name", "enp1s0", "state", "absent")))), "$.spec.network.configRef"},
		"an inline configuration without one": {up, m("inline", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", absent))), "$.spec.network.inline.nmstate"},
	} {
		t.Run(name, func(t *testing.T) {
			want := unavailable
			want.Field = test.field
			requireOneIssue(t, admission(guest(test.network), libvirtLab(test.interfaces)...), want)
		})
	}
	if issues := admission(guest(attached), libvirtLab(up)...); len(issues) != 0 {
		t.Fatalf("a Machine with one ethernet interface up was refused: %v", issues)
	}
	installed := guest(api.Value{})
	installed = installed.WithSpec(installed.Spec().With("os", m("provided", false, "installProfileRef", "rhel")).With("hardware", m("nics", list(m("name", "enp1s0")), "rootDevice", m("deviceName", "/dev/vda"))))
	issues := admission(installed, libvirtLab(up)...)
	if len(issues) != 1 || issues[0].Field != "$.spec.network.installAddressRef" ||
		issues[0].Message != "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported" {
		t.Fatalf("an installed Anaconda Machine without a network: %#v", issues)
	}
	ready := object(api.Machine, "ready", m("substrate", m("providerRef", "lab"), "os", m("provided", true)))
	if issues := admission(ready, libvirtLab(up)...); mentions(issues, "$.spec.network") {
		t.Fatalf("an OS-ready Machine was asked for a network: %v", issues)
	}
	server, catalog := fixture()
	server = server.WithSpec(server.Spec().Without("network"))
	if issues := admission(server, without(catalog, server)...); mentions(issues, "$.spec.network") {
		t.Fatalf("a bare-metal Machine was asked for a domain interface: %v", issues)
	}
}

// without is every object of catalog other than o.
func without(catalog api.Catalog, o api.Object) []api.Object {
	var objects []api.Object
	for _, existing := range catalog.Objects() {
		if existing.Identity() != o.Identity() {
			objects = append(objects, existing)
		}
	}
	return objects
}

// installOn is the lab guest installing at address on enp1s0.
func installOn(address string) api.Object {
	return guest(m("configRef", "net", "installAddressRef", "ip",
		"addresses", list(m("name", "ip", "address", address, "interface", "enp1s0"))))
}

// A managed Network's bridge holds its host address, and an IPv4 prefix shorter
// than /31 reserves its network and broadcast addresses, so none of them
// installs a guest; a /32 assignment also differs from its Network's prefix.
func TestAnInstallAddressIsNeitherTheBridgeNorItsNetworkOrBroadcast(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	lab[2] = lab[2].WithSpec(lab[2].Spec().With("machineNetwork", list(m("cidr", "192.0.2.0/24"), m("cidr", "198.51.100.0/24"))))
	lab = append(lab, object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24"))))
	for address, message := range map[string]string{
		"198.51.100.10/24": "install address must lie inside managed Network/net",
		"192.0.2.1/24":     "the install address is the host address of managed Network/net on InfraProvider/lab",
		"192.0.2.0/32":     "the install address is the network address of managed Network/net on InfraProvider/lab",
		"192.0.2.255/32":   "the install address is the broadcast address of managed Network/net on InfraProvider/lab",
	} {
		t.Run(address, func(t *testing.T) {
			for _, perInterface := range []bool{false, true} {
				node := installOn(address)
				if perInterface {
					node = node.WithSpec(node.Spec().WithPath(list(m("nicRef", "enp1s0", "segmentRef", "net")), "substrate", "libvirt", "nics"))
				}
				want := api.Issue{Code: "api.invariant", Field: "$.spec.network.installAddressRef", Message: message,
					Remediation: "assign Machine/guest an install address inside 192.0.2.0/24 other than 192.0.2.1, 192.0.2.0 and 192.0.2.255"}
				if issues := admission(node, lab...); !slices.Contains(issues, want) {
					t.Fatalf("issues = %#v, want %#v among them", issues, want)
				}
			}
		})
	}
	if issues := admission(installOn("192.0.2.11/24"), lab...); len(issues) != 0 {
		t.Fatalf("a host address inside the bridge's prefix was refused: %v", issues)
	}
}

// An IPv4 prefix shorter than /31 reserves its first and last addresses, so an
// interface assignment is never either; a /31, a /32 and IPv6 reserve neither.
func TestAnIPv4AssignmentIsAHostAddressOfItsPrefix(t *testing.T) {
	node, catalog := fixture()
	compose := func(address string) []api.Issue {
		edited := node.WithSpec(node.Spec().WithPath(list(m("name", "primary", "address", address, "interface", "eth0")), "network", "addresses"))
		_, issues := ComposeNetwork(edited, catalog)
		return issues
	}
	for _, address := range []string{"192.0.2.0/24", "192.0.2.255/24"} {
		t.Run(address, func(t *testing.T) {
			requireOneIssue(t, compose(address), api.Issue{Code: "api.value", Field: "$.spec.network.addresses[0].address",
				Message:     "an IPv4 assignment is a host address of its prefix, never its network or broadcast address",
				Remediation: "correct spec.network.addresses[0].address on Machine/node to a host address inside its prefix"})
		})
	}
	for _, address := range []string{"192.0.2.0/31", "192.0.2.255/32", "2001:db8::/64"} {
		if issues := compose(address); len(issues) != 0 {
			t.Fatalf("%s was refused: %v", address, issues)
		}
	}
}

func TestAMachineNameHasNoBlockDerivedLimit(t *testing.T) {
	validate := func(name string) []api.Issue {
		return admission(object(api.Machine, name, m("os", m("provided", true))), object(api.Environment, "env", m("domains", m("base", "example.test"))))
	}
	if issues := validate(strings.Repeat("a", 63)); len(issues) != 0 {
		t.Fatalf("a 63-byte name was refused: %v", issues)
	}
	for _, invalid := range []string{strings.Repeat("a", 70), "Rhel_01"} {
		if issues := validate(invalid); len(issues) != 0 {
			t.Fatalf("the invalid name %q got a per-kind diagnostic beside the compiler's: %v", invalid, issues)
		}
	}
}

// The derived fqdn contact is <name>.<domain>, which is a DNS name only when
// the Machine name is a label, so an invalid name derives no contact, and no
// session address falls back to one, rather than repeat the name's refusal.
func TestAnInvalidMachineNameDerivesNoContact(t *testing.T) {
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	for name, want := range map[string]string{"rhel-01": "rhel-01.example.test", "Rhel_01": ""} {
		machine := object(api.Machine, name, m("os", m("provided", true)))
		normalized, _ := Normalize(machine, api.NewCatalog([]api.Object{machine, env}))
		fqdn, _ := namedValue(normalized.Spec().Get("network", "addresses"), "fqdn")
		if got := fqdn.Get("address").Text(); got != want {
			t.Fatalf("%s derived the contact %q, want %q", name, got, want)
		}
		if want == "" && normalized.Spec().Has("access", "ssh", "addressRef") {
			t.Fatalf("%s dials the contact it does not derive: %v", name, normalized.Spec().Get("access"))
		}
	}
}

// dnsOfLength is a valid DNS name of exactly n bytes, in labels of at most 63.
func dnsOfLength(n int) string {
	var labels []string
	for n > 0 {
		size := min(63, n)
		if n-size == 1 {
			size--
		}
		labels = append(labels, strings.Repeat("a", size))
		n -= size + 1
	}
	return strings.Join(labels, ".")
}

// The derived contact <name>.<domain> is bounded like any DNS name: a valid
// name under a valid domain that composes more than 253 bytes refuses at the
// name, naming the composed contact and the domain field it came from, and
// derives neither the contact nor a session address dialing it.
func TestAnOverlongDerivedContactRefusesAtTheName(t *testing.T) {
	for _, row := range []struct {
		domains api.Value
		field   string
	}{{m("base", dnsOfLength(245)), "spec.domains.base"}, {m("base", "example.test", "machines", dnsOfLength(245)), "spec.domains.machines"}} {
		machine := object(api.Machine, "rhel-01", m("os", m("provided", true)))
		within := object(api.Environment, "env", m("domains", row.domains))
		normalized, _ := Normalize(machine, api.NewCatalog([]api.Object{machine, within}))
		if fqdn, _ := namedValue(normalized.Spec().Get("network", "addresses"), "fqdn"); len(fqdn.Get("address").Text()) != 253 {
			t.Fatalf("the contact of exactly 253 bytes was not derived: %v", normalized.Spec().Get("network"))
		}
		if issues := admission(machine, within); len(issues) != 0 {
			t.Fatalf("a derived contact of exactly 253 bytes was refused: %v", issues)
		}
		domain := row.domains.Get("machines").Text()
		if domain == "" {
			domain = row.domains.Get("base").Text()
		}
		over := object(api.Environment, "env", m("domains", row.domains.With(strings.TrimPrefix(row.field, "spec.domains."), api.StringValue("b."+domain))))
		normalized, _ = Normalize(machine, api.NewCatalog([]api.Object{machine, over}))
		if _, derived := namedValue(normalized.Spec().Get("network", "addresses"), "fqdn"); derived || normalized.Spec().Has("access", "ssh", "addressRef") {
			t.Fatalf("an over-long contact was derived or dialed: %v", normalized.Spec())
		}
		requireOneIssue(t, admission(machine, over), api.Issue{Code: "api.invariant", Field: "$.metadata.name",
			Message:     "the derived fqdn contact rhel-01.b." + domain + " exceeds a DNS name's 253 bytes or a label's 63",
			Remediation: "shorten the name of Machine/rhel-01 or the Environment's spec.domains.machines (or spec.domains.base, its default), or author an fqdn contact in spec.network.addresses"})
		dialing := machine.WithSpec(machine.Spec().WithPath(m("addressRef", "fqdn", "auth", m("operatorIdentity", m())), "access", "ssh"))
		if normalized, _ := Normalize(dialing, api.NewCatalog([]api.Object{dialing, over})); normalized.Spec().Has("access", "ssh", "addressRef") {
			t.Fatalf("an authored reference to the refused contact was kept: %v", normalized.Spec().Get("access"))
		}
	}
}

// An installed Machine's access is derived from the fleet key only when that
// key names an sshKeyPair Secret; otherwise the Environment's refusal is the
// only one, with no derived reference repeating it.
func TestAnUnresolvedFleetKeyDerivesNoAccess(t *testing.T) {
	node, catalog := fixture()
	spec := node.Spec()
	node = node.WithSpec(spec.With("os", spec.Get("os").With("installProfileRef", api.StringValue("rhel"))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet")))
	objects := []api.Object{node, env, object(api.MachineInstallProfile, "rhel", m())}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.Machine && existing.Kind() != api.Environment {
			objects = append(objects, existing)
		}
	}
	for name, secret := range map[string]api.Value{"no Secret": {}, "an opaque Secret": m("type", "opaque"), "an sshKeyPair Secret": m("type", "sshKeyPair")} {
		t.Run(name, func(t *testing.T) {
			all := slices.Clone(objects)
			if secret.Present() {
				all = append(all, object(api.Secret, "fleet", secret))
			}
			normalized, _ := Normalize(node, api.NewCatalog(all))
			access := normalized.Spec().Get("access")
			if secret.Get("type").Text() != "sshKeyPair" {
				if access.Present() {
					t.Fatalf("access was derived from an unresolved fleet key: %v", access)
				}
				return
			}
			if access.Get("ssh", "user").Text() != "bootwright" || access.Get("ssh", "auth", "privateKeyRef").Text() != "fleet" || access.Get("rootLogin").Text() != "keep" {
				t.Fatalf("access = %v", access)
			}
		})
	}
}

// A libvirt provider derives every interface's MAC and realizes the Machine's
// management controller as its emulated BMC, so a Machine of a libvirt provider
// has no MAC or controller to author: those fields belong to the bare-metal
// arm, and authoring that arm on a libvirt provider refuses at the arm. A NIC
// name alone is admitted, and a bare-metal Machine keeps its MACs and
// controller.
func TestALibvirtMachineAuthorsOnlyItsOwnArm(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	attached := installOn("192.0.2.11/24")
	with := func(hardware, arm api.Value) api.Object {
		return attached.WithSpec(attached.Spec().With("hardware", hardware).With("substrate", arm))
	}
	mismatch := api.Issue{Code: "api.invariant", Field: "$.spec.substrate.baremetal",
		Message:     "the Machine's substrate arm baremetal does not match the libvirt arm of its provider InfraProvider/lab",
		Remediation: "move the fields of spec.substrate.baremetal to spec.substrate.libvirt on Machine/guest, or select a provider with the baremetal arm"}
	nics := m("nics", list(m("name", "enp1s0")))
	for name, arm := range map[string]api.Value{
		"a MAC and a controller":   m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1", "credentialsRef", "bmc"), "nics", list(m("nicRef", "enp1s0", "macAddress", "52:54:00:00:00:01"))),
		"a MAC alone":              m("nics", list(m("nicRef", "enp1s0", "macAddress", "52:54:00:00:00:01"))),
		"an incomplete controller": m("bmc", m("address", "bmc.example.test", "virtualMedia", m("tls", m()))),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := admission(with(nics, m("providerRef", "lab", "baremetal", arm)), lab...); !slices.Contains(issues, mismatch) {
				t.Fatalf("the bare-metal arm on a libvirt provider: %#v", issues)
			}
		})
	}
	for _, arm := range []string{substrate.ArmLibvirt, substrate.ArmKubeVirt, substrate.ArmVSphere} {
		armless := api.Issue{Code: "api.invariant", Field: "$.spec.substrate." + arm,
			Message:     "a " + arm + " arm requires the provider whose arm it matches; only a bare-metal arm stands without a provider",
			Remediation: "set spec.substrate.providerRef on Machine/guest, or remove spec.substrate." + arm}
		if issues := admission(with(nics, m(arm, m("profileRef", "small"))), lab...); !slices.Contains(issues, armless) {
			t.Fatalf("a %s arm without a provider: %#v", arm, issues)
		}
	}
	if issues := admission(with(nics, m("providerRef", "lab", "libvirt", m("profileRef", "small"))), lab...); len(issues) != 0 {
		t.Fatalf("a NIC name alone was refused: %v", issues)
	}
	server, catalog := fixture()
	if issues := admission(server, append(without(catalog, server), attached, lab[0], lab[1])...); len(issues) != 0 {
		t.Fatalf("a bare-metal Machine's MAC and controller were refused: %v", issues)
	}
	twin := object(api.Machine, "twin", server.Spec())
	if issues := admission(server, append(without(catalog, server), twin)...); !slices.Contains(issues, api.Issue{Code: "api.invariant", Field: "$.spec.substrate.baremetal.nics[0].macAddress",
		Message: "authored hardware MACs must be unique across Machines", Remediation: "correct spec.substrate.baremetal.nics[0].macAddress on Machine/node, a MAC Machine/twin also declares"}) {
		t.Fatalf("a MAC two bare-metal Machines declare: %#v", issues)
	}
}

// A Machine no provider realizes, such as a bastion, is operator-owned
// hardware whose power is reached through the controller it authors, so its
// bare-metal arm stands without a provider and keeps every rule a provided arm
// has: one resolved MAC per declared NIC and an exact controller system.
func TestAProviderlessMachineAuthorsItsBareMetalArm(t *testing.T) {
	arm := func(nicRef, address string) api.Object {
		return object(api.Machine, "bastion", m("os", m("provided", true), "hardware", m("nics", list(m("name", "eno1"))),
			"substrate", m("baremetal", m("bmc", m("address", address, "credentialsRef", "bastion-bmc"),
				"nics", list(m("nicRef", nicRef, "macAddress", "02:00:00:00:20:01")), "rootDevice", m("wwn", "0x5000c500a1b2c3d4")))))
	}
	environment := object(api.Environment, "env", m("domains", m("base", "example.test")))
	if issues := admission(arm("eno1", "https://bastion-bmc.example.test/redfish/v1/Systems/1"), environment); len(issues) != 0 {
		t.Fatalf("a provider-less bare-metal arm was refused: %#v", issues)
	}
	if issues := admission(arm("eno9", "https://bastion-bmc.example.test/redfish/v1/Systems/1"), environment); slices.ContainsFunc(issues, func(issue api.Issue) bool {
		return issue.Field == "$.spec.substrate.baremetal.nics[0].nicRef"
	}) {
		t.Fatalf("an unresolved NIC, which the shared reference check reports, was refused again: %#v", issues)
	}
	if issues := admission(arm("eno1", "https://bastion-bmc.example.test/redfish/v1"), environment); !slices.ContainsFunc(issues, func(issue api.Issue) bool {
		return issue.Code == "api.invariant" && issue.Field == "$.spec.substrate.baremetal.bmc.address"
	}) {
		t.Fatalf("a controller naming no exact system was admitted: %#v", issues)
	}
}

// A KubeVirt provider derives every interface's MAC exactly as a libvirt one
// does, so the bare-metal arm that carries MACs refuses on it at the arm, and a
// NIC name alone is admitted.
func TestAKubeVirtMachineAuthorsNoMAC(t *testing.T) {
	provider := object(api.InfraProvider, "lab", m("kubevirt", m("kubeconfigRef", "host", "namespace", "guests")))
	hardware := m("nics", list(m("name", "eth0"), m("name", "eth1")))
	guest := object(api.Machine, "guest", m("substrate", m("providerRef", "lab", "baremetal", m("nics", list(m("nicRef", "eth0", "macAddress", "02:00:00:00:10:01")))), "os", m("provided", false), "hardware", hardware))
	catalog := api.NewCatalog([]api.Object{guest, provider})
	requireOneIssue(t, validateArm(guest, provider, true, substrate.ArmKubeVirt), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.baremetal",
		Message:     "the Machine's substrate arm baremetal does not match the kubevirt arm of its provider InfraProvider/lab",
		Remediation: "move the fields of spec.substrate.baremetal to spec.substrate.kubevirt on Machine/guest, or select a provider with the baremetal arm"})
	named := guest.WithSpec(guest.Spec().With("substrate", m("providerRef", "lab", "kubevirt", m("profileRef", "small"))))
	if issues := validateHardware(named, catalog, substrate.ArmKubeVirt); len(issues) != 0 {
		t.Fatalf("NIC names alone were refused: %v", issues)
	}
}

// A bare-metal attachment configures nothing, so a bare-metal Machine needs no
// attachment selection. A libvirt NIC's attachment is its domain interface's
// network, so with two attachments to choose from each NIC names one.
func TestABaremetalMachineSelectsNoAttachment(t *testing.T) {
	inline := guest(m("inline", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet"))))))
	requireOneIssue(t, admission(inline, twoSegmentLab(list())...), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.nics",
		Message:     "NIC enp1s0 of Machine/guest selects no segment, and InfraProvider/lab declares 2 segments, not exactly one that every NIC defaults to",
		Remediation: "add a spec.substrate.libvirt.nics entry with nicRef enp1s0 and a segmentRef naming a segment of InfraProvider/lab on Machine/guest"})
	server, catalog := fixture()
	objects := without(catalog, server)
	for i, existing := range objects {
		if existing.Kind() == api.InfraProvider {
			objects[i] = existing.WithSpec(existing.Spec().Without("segments"))
		}
	}
	if issues := admission(server, objects...); len(issues) != 0 {
		t.Fatalf("a bare-metal Machine without an attachment was refused: %v", issues)
	}
}

// An authored fqdn contact is a DNS name in the one grammar the API states.
func TestAnFQDNContactIsADNSName(t *testing.T) {
	node, catalog := fixture()
	contact := func(address string) []api.Issue {
		edited := node.WithSpec(node.Spec().WithPath(list(m("name", "fqdn", "address", address), m("name", "primary", "address", "192.0.2.11/24", "interface", "eth0")), "network", "addresses"))
		return admission(edited, without(catalog, edited)...)
	}
	for _, address := range []string{"Rhel.example.test", "a-.example"} {
		requireOneIssue(t, contact(address), api.Issue{Code: "api.invariant", Field: "$.spec.network.addresses[0]", Message: "fqdn must be an unassigned DNS contact",
			Remediation: "correct spec.network.addresses[0] on Machine/node to a lowercase DNS name with no interface"})
	}
	if issues := contact("rhel-01.example.test"); len(issues) != 0 {
		t.Fatalf("a DNS name was refused: %v", issues)
	}
}

// A MAC is authored in its substrate arm and names one declared hardware NIC,
// once; a vSphere MAC lies in the manual assignment range.
func TestAMACNamesOneDeclaredHardwareNIC(t *testing.T) {
	server, catalog := fixture()
	edit := func(nics api.Value) []api.Issue {
		edited := server.WithSpec(server.Spec().WithPath(nics, "substrate", "baremetal", "nics"))
		return validateHardware(edited, catalog, substrate.ArmBaremetal)
	}
	if issues := edit(list(m("nicRef", "eth9", "macAddress", "02:00:00:00:00:09"))); len(issues) != 0 {
		t.Fatalf("an unresolved nicRef, which the shared reference check reports, was refused again: %v", issues)
	}
	requireOneIssue(t, edit(list(m("nicRef", "eth0", "macAddress", "02:00:00:00:00:01"), m("nicRef", "eth0", "macAddress", "02:00:00:00:00:02"))), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.baremetal.nics[1].nicRef",
		Message:     "each hardware NIC has one MAC",
		Remediation: "remove the repeated spec.substrate.baremetal.nics[1] entry"})
	virtual := func(mac string) []api.Issue {
		machine := object(api.Machine, "vm", m("substrate", m("providerRef", "lab", "vsphere", m("nics", list(m("nicRef", "eth0", "macAddress", mac)))), "hardware", m("nics", list(m("name", "eth0")))))
		return validateHardware(machine, api.NewCatalog([]api.Object{machine}), substrate.ArmVSphere)
	}
	requireOneIssue(t, virtual("02:00:00:00:00:01"), api.Issue{Code: "api.invariant", Field: "$.spec.substrate.vsphere.nics[0].macAddress",
		Message:     "vSphere MAC must be in the manual assignment range",
		Remediation: "set spec.substrate.vsphere.nics[0].macAddress to a MAC from 00:50:56:00:00:00 to 00:50:56:3f:ff:ff"})
	if issues := virtual("00:50:56:00:00:01"); len(issues) != 0 {
		t.Fatalf("a MAC in the vSphere range was refused: %v", issues)
	}
}

// A libvirt or KubeVirt provider creates one vNIC per declared NIC, so a
// Machine it realizes declares at least one, and a profile names a machine
// profile of the selected provider.
func TestAVirtualMachineDeclaresItsNICsAndNamesAProviderProfile(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	provider := lab[1]
	bare := installOn("192.0.2.11/24")
	bare = bare.WithSpec(bare.Spec().Without("hardware"))
	requireOneIssue(t, validateSubstrate(api.NewCatalog(lab), bare, provider, substrate.ArmLibvirt), api.Issue{Code: "api.invariant", Field: "$.spec.hardware.nics",
		Message:     "a libvirt provider creates one vNIC per spec.hardware.nics entry, in order, and Machine/guest declares none",
		Remediation: "declare each vNIC of Machine/guest in spec.hardware.nics, in device order, such as one entry named enp1s0"})
	unknown := installOn("192.0.2.11/24")
	unknown = unknown.WithSpec(unknown.Spec().WithPath(api.StringValue("missing"), "substrate", "libvirt", "profileRef"))
	if issues := validateSubstrate(api.NewCatalog(lab), unknown, provider, substrate.ArmLibvirt); len(issues) != 0 {
		t.Fatalf("an unresolved profileRef, which the shared reference check reports, was refused again: %v", issues)
	}
}

func TestInstallAttachmentContainmentFollowsVLANAndBondParents(t *testing.T) {
	for _, network := range []struct {
		iface      string
		interfaces api.Value
	}{
		{"enp1s0.100", list(m("name", "enp1s0", "type", "ethernet"), m("name", "enp1s0.100", "type", "vlan", "vlan", m("base-iface", "enp1s0", "id", api.IntegerValue("100"))))},
		{"bond0", list(m("name", "enp1s0", "type", "ethernet"), m("name", "bond0", "type", "bond", "link-aggregation", m("mode", "active-backup", "port", api.StringList("enp1s0"))))},
	} {
		node := installOn("192.0.2.1/24")
		node = node.WithSpec(node.Spec().With("network", node.Spec().Get("network").With("addresses", list(m("name", "ip", "address", "192.0.2.1/24", "interface", network.iface)))).WithPath(list(m("nicRef", "enp1s0", "segmentRef", "net")), "substrate", "libvirt", "nics"))
		issues := admission(node, libvirtLab(network.interfaces)...)
		if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
			return issue.Field == "$.spec.network.installAddressRef" && strings.Contains(issue.Message, "host address of managed Network/net")
		}) {
			t.Fatalf("%s containment = %v", network.iface, issues)
		}
	}
}

// An OS-ready Machine of a one-attachment libvirt provider may declare its
// hardware NICs: normalization defaults no arm nics entry for it, so the rule
// that an OS-ready Machine declares contacts only has nothing to refuse.
func TestAnOSReadyVirtualMachineMayDeclareHardwareNICs(t *testing.T) {
	ready := object(api.Machine, "ready", m("substrate", m("providerRef", "lab", "libvirt", m()), "os", m("provided", true), "hardware", m("nics", list(m("name", "enp1s0")))))
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	if issues := admission(ready, lab...); len(issues) != 0 {
		t.Fatalf("an OS-ready libvirt Machine with hardware NICs was refused: %v", issues)
	}
	normalized, _ := Normalize(ready, api.NewCatalog(append([]api.Object{ready}, lab...)))
	if normalized.Spec().Has("substrate", "libvirt", "nics") {
		t.Fatalf("an OS-ready Machine was given arm nics: %v", normalized.Spec().Get("substrate", "libvirt", "nics"))
	}
}

// A non-provided libvirt or KubeVirt Machine's NIC name is the interface name
// the installer pins when no interface matches the NIC, so it is one.
func TestAVirtualMachineNICNameIsAnInterfaceName(t *testing.T) {
	machine := installOn("192.0.2.11/24")
	machine = machine.WithSpec(machine.Spec().WithPath(list(m("name", "enp1s0"), m("name", "uplink port")), "hardware", "nics"))
	requireOneIssue(t, admission(machine, libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))...), api.Issue{Code: "api.invariant", Field: "$.spec.hardware.nics[1].name",
		Message:     "NIC uplink port of Machine/guest is a libvirt vNIC the installer pins under its NIC name unless an interface matches it, and " + interfaceNameRule,
		Remediation: "rename NIC uplink port in spec.hardware.nics on Machine/guest to an interface name the guest may use, such as enp2s0, and update every nicRef that names it"})
	ready := object(api.Machine, "ready", m("substrate", m("providerRef", "lab", "libvirt", m()), "os", m("provided", true), "hardware", m("nics", list(m("name", "uplink port")))))
	if issues := admission(ready, libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))...); len(issues) != 0 {
		t.Fatalf("an OS-ready Machine's NIC name was refused: %v", issues)
	}
}

// A provider with no network attachment cannot attach any vNIC, so the step
// is to declare one on the provider, not to name an attachment that is absent.
func TestAProviderWithoutAttachmentsIsToldToDeclareOne(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	lab = slices.DeleteFunc(lab, func(o api.Object) bool { return o.Kind() == api.Network })
	issues := admission(installOn("192.0.2.11/24"), lab...)
	want := api.Issue{Code: "api.invariant", Field: "$.spec.substrate.libvirt.nics",
		Message:     "Machine/guest cannot attach its vNICs: InfraProvider/lab declares no segment",
		Remediation: "declare a segment with a libvirt arm in spec.segments of InfraProvider/lab, or a Network with spec.provided false whose spec.libvirt.providerRef names lab"}
	if !slices.Contains(issues, want) {
		t.Fatalf("issues = %#v, want %#v", issues, want)
	}
}

// The D197 refusal points at the field that authors the mac-address: the
// override that sets it, else the network configuration.
func TestAVirtualMACRefusalPointsAtTheFieldThatAuthorsIt(t *testing.T) {
	network := func(extra ...any) api.Value {
		return m(append([]any{"configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0"))}, extra...)...)
	}
	plain := list(m("name", "enp1s0", "type", "ethernet"))
	override := m("interfaces", list(m("name", "enp1s0", "mac-address", "52:54:00:00:00:01")))
	requireOneIssue(t, admission(guest(network("overrides", override)), libvirtLab(plain)...), api.Issue{Code: "api.invariant", Field: "$.spec.network.overrides",
		Message:     "ethernet interface enp1s0 of Machine/guest authors a mac-address, but a libvirt provider derives each vNIC's MAC from the context, Machine and NIC names",
		Remediation: "remove mac-address from interface enp1s0 in spec.network.overrides of Machine/guest, and match the interface to its NIC with spec.network.interfaceMatches if their names differ"})
	authored := list(m("name", "enp1s0", "type", "ethernet", "mac-address", "52:54:00:00:00:01"))
	requireOneIssue(t, admission(guest(network("overrides", m("interfaces", list(m("name", "enp1s0", "description", "uplink"))))), libvirtLab(authored)...), api.Issue{Code: "api.invariant", Field: "$.spec.network.configRef",
		Message:     "ethernet interface enp1s0 of Machine/guest authors a mac-address, but a libvirt provider derives each vNIC's MAC from the context, Machine and NIC names",
		Remediation: "remove mac-address from interface enp1s0 in the network configuration of Machine/guest, and match the interface to its NIC with spec.network.interfaceMatches if their names differ"})
}
