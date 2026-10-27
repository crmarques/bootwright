package machine

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

// libvirtLab is a libvirt provider on its host Machine, with one managed
// attachment on 192.0.2.1/24, a network configuration presenting interfaces,
// an Anaconda install profile and an Environment domain.
func libvirtLab(interfaces api.Value) []api.Object {
	return []api.Object{
		object(api.Machine, "host", m("os", m("provided", true))),
		object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulation", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small"))),
			"networkAttachments", list(m("name", "net", "libvirt", m("bridge", "virbr-lab", "management", "managed", "address", "192.0.2.1/24"))))),
		object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", interfaces))),
		object(api.MachineInstallProfile, "rhel", m("installer", m("anaconda", m("imageRef", "image")))),
		object(api.Environment, "env", m("domains", m("base", "example.test"))),
	}
}

// guest is an installer-provisioned Machine of the lab provider with network.
func guest(network api.Value) api.Object {
	spec := m("substrate", m("providerRef", "lab", "libvirt", m("profileRef", "small")), "os", m("provided", false))
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

// A libvirt domain attaches one interface per available ethernet interface of
// the configuration its Machine selects, so a non-provided Machine with none
// refuses at admission rather than at plan, after composition and its
// overrides. An installed Anaconda Machine without a network keeps the one
// refusal its installation rule already gives.
func TestALibvirtMachineNeedsAnAvailableEthernetInterface(t *testing.T) {
	up := list(m("name", "enp1s0", "type", "ethernet", "state", "up"))
	absent := list(m("name", "enp1s0", "type", "ethernet", "state", "absent"))
	attached := m("configRef", "net", "attachmentRef", "net")
	unavailable := api.Issue{Code: "api.invariant", Field: "$.spec.network.configRef",
		Message:     "the network configuration presents no available ethernet interface, so the domain would have no interface",
		Remediation: "declare an ethernet interface that is not absent or ignored in the NMState Machine/guest selects, or select another configuration"}
	t.Run("no network", func(t *testing.T) {
		requireOneIssue(t, admission(guest(api.Value{}), libvirtLab(up)...), api.Issue{Code: "api.invariant", Field: "$.spec.network",
			Message:     "a Machine a libvirt provider realizes attaches one interface per available ethernet interface of its network configuration, and this Machine selects none",
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
		"an inline configuration without one": {up, m("inline", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", absent)), "attachmentRef", "net"), "$.spec.network.inline.nmstate"},
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
	installed = installed.WithSpec(installed.Spec().With("os", m("provided", false, "installProfileRef", "rhel")).With("hardware", m("rootDevice", m("deviceName", "/dev/vda"))))
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
	return guest(m("configRef", "net", "attachmentRef", "net", "installAddressRef", "ip",
		"addresses", list(m("name", "ip", "address", address, "interface", "enp1s0"))))
}

// A managed bridge holds its own host address, and an IPv4 prefix shorter than
// /31 reserves its network and broadcast addresses, so none of them installs a
// guest. A /32 assignment reserves nothing of its own, so only the bridge's
// prefix refuses it.
func TestAnInstallAddressIsNeitherTheBridgeNorItsNetworkOrBroadcast(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	for address, message := range map[string]string{
		"192.0.2.1/24":   "the install address is the managed bridge's own host address on InfraProvider/lab",
		"192.0.2.0/32":   "the install address is the network address of the managed bridge's prefix on InfraProvider/lab",
		"192.0.2.255/32": "the install address is the broadcast address of the managed bridge's prefix on InfraProvider/lab",
	} {
		t.Run(address, func(t *testing.T) {
			requireOneIssue(t, admission(installOn(address), lab...), api.Issue{Code: "api.invariant", Field: "$.spec.network.installAddressRef", Message: message,
				Remediation: "assign Machine/guest an install address inside 192.0.2.0/24 other than 192.0.2.1, 192.0.2.0 and 192.0.2.255"})
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
	if issues := admission(arm("eno9", "https://bastion-bmc.example.test/redfish/v1/Systems/1"), environment); !slices.ContainsFunc(issues, func(issue api.Issue) bool {
		return issue.Code == "api.reference" && issue.Field == "$.spec.substrate.baremetal.nics[0].nicRef"
	}) {
		t.Fatalf("an unresolved NIC was admitted: %#v", issues)
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
// attachment selection; one it authors still names an attachment of its
// provider. A libvirt Machine's attachment is its domain's network, so it
// still selects one.
func TestABaremetalMachineSelectsNoAttachment(t *testing.T) {
	inline := guest(m("inline", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet"))))))
	requireOneIssue(t, admission(inline, libvirtLab(list())...), api.Issue{Code: "api.invariant", Field: "$.spec.network.attachmentRef",
		Message:     "provider-backed configured networks require an explicit attachment unless the unique matching default applies",
		Remediation: "set spec.network.attachmentRef on Machine/guest to an attachment of InfraProvider/lab"})
	server, catalog := fixture()
	objects := without(catalog, server)
	for i, existing := range objects {
		if existing.Kind() == api.InfraProvider {
			objects[i] = existing.WithSpec(existing.Spec().Without("networkAttachments"))
		}
	}
	if issues := admission(server, objects...); len(issues) != 0 {
		t.Fatalf("a bare-metal Machine without an attachment was refused: %v", issues)
	}
	named := server.WithSpec(server.Spec().WithPath(api.StringValue("missing"), "network", "attachmentRef"))
	if issues := admission(named, objects...); len(issues) != 1 || issues[0].Code != "api.reference" || issues[0].Field != "$.spec.network.attachmentRef" {
		t.Fatalf("an unresolved attachmentRef: %#v", issues)
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
	requireOneIssue(t, edit(list(m("nicRef", "eth9", "macAddress", "02:00:00:00:00:09"))), api.Issue{Code: "api.reference", Field: "$.spec.substrate.baremetal.nics[0].nicRef",
		Message:     "a MAC must name a declared hardware NIC",
		Remediation: "set spec.substrate.baremetal.nics[0].nicRef to the name of a spec.hardware.nics entry"})
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

// A libvirt or KubeVirt provider creates every interface of its Machine, so
// there is no hardware NIC to bind, and a profile names a machine profile of
// the selected provider.
func TestAVirtualMachineBindsNoNICAndNamesAProviderProfile(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	provider := lab[1]
	bound := installOn("192.0.2.11/24")
	bound = bound.WithSpec(bound.Spec().WithPath(m("eth0", "enp1s0"), "network", "interfaceBinding"))
	requireOneIssue(t, validateSubstrate(bound, provider, substrate.ArmLibvirt), api.Issue{Code: "api.invariant", Field: "$.spec.network.interfaceBinding",
		Message:     "a libvirt provider creates every interface of this Machine, so there is no hardware NIC to bind",
		Remediation: "remove spec.network.interfaceBinding from Machine/guest"})
	unknown := installOn("192.0.2.11/24")
	unknown = unknown.WithSpec(unknown.Spec().WithPath(api.StringValue("missing"), "substrate", "libvirt", "profileRef"))
	requireOneIssue(t, validateSubstrate(unknown, provider, substrate.ArmLibvirt), api.Issue{Code: "api.reference", Field: "$.spec.substrate.libvirt.profileRef",
		Message:     "profile must name a machine profile on the selected provider",
		Remediation: "set spec.substrate.libvirt.profileRef to the name of a machineProfiles entry of the selected provider"})
}
