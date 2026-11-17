package machine

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}
func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var v api.Value
		switch x := kv[i+1].(type) {
		case api.Value:
			v = x
		case string:
			v = api.StringValue(x)
		case bool:
			v = api.BoolValue(x)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}
func list(v ...api.Value) api.Value { return api.ListValue(v...) }
func fixture() (api.Object, api.Catalog) {
	provider := object(api.InfraProvider, "metal", m("baremetal", m("defaults", m("bmc", m("credentialsRef", "bmc", "virtualMedia", m("tls", m("trust", "established"))))), "segments", list(m("name", "net", "untaggedNetworkRef", "lan", "baremetal", m()))))
	network := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0"))))))
	machine := object(api.Machine, "node", m("hardware", m("providerRef", "metal", "rootDisk", m("wwn", "0x5000c500a1b2c3d4"), "bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1", "tls", m("verification", "none")), "nics", list(m("name", "eth0", "macAddress", "02-00-00-00-00-01"))), "os", m("clusterInstalled", m()), "network", m("configRef", "net", "addresses", list(m("name", "primary", "address", "192.0.2.11", "interface", "eth0")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	lan := object(api.Network, "lan", m("provided", true, "ipv4", m("cidr", "192.0.2.0/24")))
	return machine, api.NewCatalog([]api.Object{machine, network, provider, env, lan})
}

func TestBaremetalNormalizationPreservesDeclarations(t *testing.T) {
	o, c := fixture()
	normalized, issues := Normalize(o, c)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if got := Validate(normalized, c); len(got) > 0 {
		t.Fatal(got)
	}
	if got := normalized.Spec().Get("hardware", "nics").Items()[0].Get("macAddress").Text(); got != "02:00:00:00:00:01" {
		t.Fatal(got)
	}
	if normalized.Spec().Get("hardware", "bmc", "tls", "verification").Text() != "none" {
		t.Fatal("machine TLS choice lost")
	}
	if bmc := normalized.Spec().Get("hardware", "bmc"); bmc.Has("protocol") || bmc.Has("tls", "verify") {
		t.Fatalf("a field the schema no longer has was materialized: %v", bmc)
	}
	if normalized.Spec().Get("network", "installAddressRef").Text() != "primary" {
		t.Fatal("network defaults missing")
	}
	if !normalized.Spec().Get("network", "interfaceMatches").Equal(list(m("interface", "eth0", "nicRef", "eth0"))) {
		t.Fatal("the defaulted interface match was not materialized")
	}
	native, issues := ComposeNetwork(normalized, c)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	iface := native.Get("interfaces").Items()[0]
	if iface.Get("ipv4", "address").Items()[0].Get("ip").Text() != "192.0.2.11" || iface.Get("mac-address").Text() != "02:00:00:00:00:01" {
		t.Fatal("internal address/MAC injection failed")
	}
	if normalized.Spec().Get("network").Has("nmstate") || o.Spec().Get("network").Has("installAddressRef") {
		t.Fatal("authored or canonical state replaced by composition")
	}
	local := o.WithSpec(o.Spec().WithPath(m("tls", m("trust", "importCertificate")), "hardware", "bmc", "virtualMedia"))
	local, _ = Normalize(local, c)
	tls := local.Spec().Get("hardware", "bmc", "virtualMedia", "tls")
	if tls.Get("trust").Text() != "importCertificate" || tls.Has("restoreVerificationAfterBoot") {
		t.Fatal("machine virtual-media block did not replace provider block")
	}
}

func TestMediaVerificationSetupInheritsAsExplicitProviderIntent(t *testing.T) {
	machine, catalog := fixture()
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.InfraProvider {
			objects[index] = object.WithSpec(object.Spec().
				WithPath(api.BoolValue(true), "baremetal", "defaults", "bmc", "virtualMedia", "tls", "enableVerification"))
		}
	}
	catalog = api.NewCatalog(objects)
	machine = machine.WithSpec(machine.Spec().WithPath(m("verification", "full"), "hardware", "bmc", "tls"))
	normalized, issues := Normalize(machine, catalog)
	if len(issues) != 0 || !normalized.Spec().Get("hardware", "bmc", "virtualMedia", "tls", "enableVerification").Bool() {
		t.Fatalf("provider intent lost: %v %v", normalized.Spec(), issues)
	}
	local := machine.WithSpec(machine.Spec().WithPath(api.BoolValue(false), "hardware", "bmc", "virtualMedia", "tls", "enableVerification"))
	normalized, issues = Normalize(local, catalog)
	if len(issues) != 0 || normalized.Spec().Get("hardware", "bmc", "virtualMedia", "tls", "enableVerification").Bool() {
		t.Fatalf("local opt-out lost: %v %v", normalized.Spec(), issues)
	}
}

func TestNativeCompositionRejectsHiddenAndConflictingInputs(t *testing.T) {
	o, c := fixture()
	cases := map[string]api.Value{
		"dynamic":          m("interfaces", list(m("name", "eth0", "ipv4", m("dhcp", true)))),
		"disabled":         m("interfaces", list(m("name", "eth0", "ipv4", m("enabled", false)))),
		"unavailable":      m("interfaces", list(m("name", "eth0", "state", "absent"))),
		"mac conflict":     m("interfaces", list(m("name", "eth0", "mac-address", "02:00:00:00:00:ff"))),
		"authored address": m("interfaces", list(m("name", "eth0", "ipv4", m("address", list(m("ip", "192.0.2.1")))))),
		"new missing type": m("interfaces", list(m("name", "eth1"))),
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			changed := o.WithSpec(o.Spec().WithPath(override, "network", "overrides"))
			if _, issues := ComposeNetwork(changed, c); len(issues) == 0 {
				t.Fatal("invalid composition admitted")
			}
		})
	}
	network, _ := c.Find(api.NetworkConfig, "net")
	network = network.WithSpec(network.Spec().WithPath(list(m("name", "eth0", "type", "ethernet", "ipv4", m("address", list(m("ip", "192.0.2.99"))))), "nmstate", "interfaces"))
	changed := o.WithSpec(o.Spec().WithPath(m("interfaces", list(m("name", "eth0", "ipv4", m("address", list())))), "network", "overrides"))
	if _, issues := ComposeNetwork(changed, api.NewCatalog([]api.Object{network})); len(issues) == 0 {
		t.Fatal("override hid authored static IP")
	}
}

func TestInstallAddressSelectionHasNoOrderFallback(t *testing.T) {
	o, c := fixture()
	configuration, _ := c.Find(api.NetworkConfig, "net")
	lan, _ := c.Find(api.Network, "lan")
	configuration = configuration.WithSpec(configuration.Spec().WithPath(list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet")), "nmstate", "interfaces"))
	o = o.WithSpec(o.Spec().Without("hardware").WithPath(list(m("name", "second", "address", "192.0.2.22", "interface", "eth1"), m("name", "first", "address", "192.0.2.11", "interface", "eth0")), "network", "addresses"))
	c = api.NewCatalog([]api.Object{configuration, lan})
	selected, issues := InstallAddress(o, c)
	if len(issues) != 0 || selected.Get("name").Text() != "first" {
		t.Fatal(selected, issues)
	}
	configuration = configuration.WithSpec(configuration.Spec().WithPath(m(), "nmstate", "routes"))
	c = api.NewCatalog([]api.Object{configuration, lan})
	if _, issues = InstallAddress(o, c); len(issues) == 0 {
		t.Fatal("ambiguous addresses admitted")
	}
	o = o.WithSpec(o.Spec().WithPath(api.StringValue("second"), "network", "installAddressRef"))
	selected, issues = InstallAddress(o, c)
	if len(issues) != 0 || selected.Get("name").Text() != "second" {
		t.Fatal(selected, issues)
	}
	o = o.WithSpec(o.Spec().WithPath(api.StringValue("missing"), "network", "installAddressRef"))
	if _, issues = InstallAddress(o, c); len(issues) == 0 {
		t.Fatal("invalid explicit address admitted")
	}
}

func TestNativeListMergeContract(t *testing.T) {
	base := m("items", list(m("name", "a", "x", "one"), m("name", "b", "x", "two")))
	merged, issues := mergeNative(base, m("items", list(m("name", "a", "y", "three"), m("name", "c", "x", "four"))), "$")
	if len(issues) != 0 || merged.Get("items").Len() != 3 || merged.Get("items").Items()[0].Get("x").Text() != "one" {
		t.Fatal(merged, issues)
	}
	for _, right := range []api.Value{list(api.StringValue("scalar")), list(m("name", "a"), m("x", "unnamed"))} {
		if _, issues := mergeNative(base, m("items", right), "$"); len(issues) == 0 {
			t.Fatal("unsupported native list merge admitted")
		}
	}
	if _, issues := mergeNative(m("items", list(api.StringValue("unchanged"))), m("other", true), "$"); len(issues) != 0 {
		t.Fatal("untouched native list rejected")
	}
}

func TestAccessLifecycleAndGlobalIdentities(t *testing.T) {
	o, c := fixture()
	ready := object(api.Machine, "ready", m("os", m("provided", m())))
	ready, _ = Normalize(ready, c)
	if !ready.Spec().Has("access", "ssh", "auth", "operatorIdentity") || ready.Spec().Get("access", "ssh", "addressRef").Text() != "fqdn" {
		t.Fatal("operator defaults missing")
	}
	if got := Validate(ready, c); len(got) != 0 {
		t.Fatal(got)
	}
	for name, changed := range map[string]api.Object{
		"installed authored access": o.WithSpec(o.Spec().With("os", m("install", m("profileRef", "install"))).With("access", m("local", m()))),
		"password without user":     ready.WithSpec(ready.Spec().WithPath(m("passwordRef", "password"), "access", "ssh", "auth")),
		"empty media TLS":           o.WithSpec(o.Spec().WithPath(m(), "hardware", "bmc", "virtualMedia", "tls")),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := ValidateAuthored(changed, c); len(issues) == 0 {
				t.Fatal("authored restriction ignored")
			}
		})
	}
	peer := object(api.Machine, "peer", o.Spec())
	if got := validateHardware(o, api.NewCatalog([]api.Object{o, peer}), "baremetal"); len(got) == 0 {
		t.Fatal("duplicate MAC admitted")
	}
}

// An installation proves its Machine's host key at the install address, so a
// session that authors no address dials that one rather than a name only a
// resolver knows; a declared ssh address still wins. An OS-ready Machine has
// no installation to prove anything, so it keeps its FQDN whatever its
// network selects.
func TestAnInstalledMachineDialsItsInstallAddress(t *testing.T) {
	node, fixtures := fixture()
	addresses := list(m("name", "ip", "address", "192.0.2.11", "interface", "eth0"))
	spec := node.Spec().WithPath(addresses, "network", "addresses")
	installed := node.WithSpec(spec.With("os", m("install", m("profileRef", "rhel"))))
	objects := []api.Object{object(api.MachineInstallProfile, "rhel", m()), object(api.Secret, "fleet", m("type", "sshKeyPair")),
		object(api.Environment, "env", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet")))}
	for _, existing := range fixtures.Objects() {
		if existing.Kind() != api.Machine && existing.Kind() != api.Environment {
			objects = append(objects, existing)
		}
	}
	catalog := api.NewCatalog(objects)
	dialed := func(o api.Object) api.Value {
		t.Helper()
		normalized, issues := Normalize(o, api.NewCatalog(append([]api.Object{o}, catalog.Objects()...)))
		if len(issues) != 0 {
			t.Fatal(issues)
		}
		return normalized.Spec()
	}
	effective := dialed(installed)
	if effective.Get("network", "installAddressRef").Text() != "ip" || !effective.Has("network", "addresses") {
		t.Fatalf("network = %v", effective.Get("network"))
	}
	if _, found := infrastructureservices.AddressOf(installed.WithSpec(effective), "fqdn"); !found {
		t.Fatal("the installed Machine has no FQDN to fall back from")
	}
	if got := effective.Get("access", "ssh"); got.Get("addressRef").Text() != "ip" || got.Get("user").Text() != "bootwright" ||
		got.Get("auth", "privateKeyRef").Text() != "fleet" {
		t.Fatalf("an installed Machine dials %v", got)
	}
	declared := installed.WithSpec(installed.Spec().WithPath(list(addresses.Items()[0],
		m("name", "ssh", "address", "192.0.2.50")), "network", "addresses"))
	if got := dialed(declared).Get("access", "ssh", "addressRef").Text(); got != "ssh" {
		t.Fatalf("an installed Machine that declares an ssh address dials %q", got)
	}
	ready := installed.WithSpec(spec.With("os", m("provided", m())).WithPath(api.StringValue("ip"), "network", "installAddressRef"))
	if got := dialed(ready).Get("access", "ssh", "addressRef").Text(); got != "fqdn" {
		t.Fatalf("an OS-ready Machine dials %q", got)
	}
	undeclared := installed.WithSpec(installed.Spec().WithPath(api.StringValue("missing"), "network", "installAddressRef"))
	if got := dialed(undeclared).Get("access", "ssh", "addressRef").Text(); got != "fqdn" {
		t.Fatalf("an installed Machine whose install address is undeclared dials %q", got)
	}
}

// Every authored BMC address is held to the one grammar the realized target
// reads, whether or not anything installs the Machine, and one address raises
// one issue.
func TestEveryAuthoredBMCAddressNamesOneExactSystem(t *testing.T) {
	const field = "$.spec.hardware.bmc.address"
	installed, catalog := fixture()
	provided := object(api.Machine, "bastion", m("os", m("provided", m()),
		"hardware", m("providerRef", "metal", "bmc", m("address", "https://bastion-bmc.example.test/redfish/v1/Systems/1", "credentialsRef", "bmc", "tls", m("verification", "none")))))
	for name, machine := range map[string]api.Object{"bare-metal installation": installed, "OS-ready": provided} {
		with := func(address string) []api.Issue {
			edited := machine.WithSpec(machine.Spec().WithPath(api.StringValue(address), "hardware", "bmc", "address"))
			normalized, _ := Normalize(edited, catalog)
			return Validate(normalized, catalog)
		}
		t.Run(name, func(t *testing.T) {
			if issues := with(machine.Spec().Get("hardware", "bmc", "address").Text()); len(issues) != 0 {
				t.Fatal("a canonical address was refused", issues)
			}
			for _, address := range []string{
				"redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/1",
				"https://bmc.example.test/redfish/v1/Systems/1/",
			} {
				if issues := with(address); len(issues) != 1 || issues[0].Field != field {
					t.Fatalf("%q: issues = %v", address, issues)
				}
			}
		})
	}
}

func TestNativeMergeDiagnosticsDoNotExposeMapKeys(t *testing.T) {
	key := strings.Repeat("synthetic-native-key-", 3000)
	_, issues := mergeNative(m(key, list(api.BoolValue(true))), m(key, list(api.BoolValue(false))), "$.spec.network.overrides")
	if len(issues) != 1 || issues[0].Field != "$.spec.network.overrides" || strings.Contains(issues[0].Message, key) {
		t.Fatalf("native key exposed: count%d", len(issues))
	}
}

func TestManagedLibvirtSegmentContainsTheInstallAddress(t *testing.T) {
	host := object(api.Machine, "host", m("os", m("provided", m())))
	provider := object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulation", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small")))))
	managed := managedNet("net", "lab", "192.0.2.0/24", "192.0.2.1")
	other := object(api.Network, "other", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	network := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	for address, valid := range map[string]bool{"192.0.2.11": true, "198.51.100.11": false} {
		guest := object(api.Machine, "guest", m("hardware", m("providerRef", "lab", "profileRef", "small", "nics", list(m("name", "enp1s0"))), "os", m("clusterInstalled", m()), "network", m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", address, "interface", "enp1s0")))))
		c := api.NewCatalog([]api.Object{guest, host, provider, network, env, managed, other})
		guest, _ = Normalize(guest, c)
		issues := Validate(guest, c)
		if (len(issues) == 0) != valid {
			t.Fatalf("%s: issues = %v", address, issues)
		}
	}
}

// withRootDisk is a Machine of the catalog, normalized, with its declared
// root disk replaced.
func withRootDisk(machine api.Object, catalog api.Catalog, rootDisk api.Value) (api.Object, api.Catalog) {
	machine = machine.WithSpec(machine.Spec().WithPath(rootDisk, "hardware", "rootDisk"))
	objects := []api.Object{machine}
	for _, existing := range catalog.Objects() {
		if existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	machine, _ = Normalize(machine, api.NewCatalog(objects))
	objects[0] = machine
	return machine, api.NewCatalog(objects)
}

// The disks a libvirt domain presents are created by the provider and every
// installer on them writes /dev/vda, so a libvirt Machine declares no root disk
// and any of its selectors or hints refuses at hardware.rootDisk. A physical
// machine's own disks may carry every one of them, so bare metal keeps them all.
func TestARootDiskFollowsItsProvider(t *testing.T) {
	host := object(api.Machine, "host", m("os", m("provided", m())))
	provider := object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulation", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small")))))
	managed := managedNet("net", "lab", "192.0.2.0/24", "192.0.2.1")
	network := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	guest := object(api.Machine, "guest", m("hardware", m("providerRef", "lab", "profileRef", "small", "nics", list(m("name", "enp1s0"))), "os", m("clusterInstalled", m()), "network", m("configRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11", "interface", "enp1s0")))))
	virtual := api.NewCatalog([]api.Object{guest, host, provider, network, env, managed})
	server, physical := fixture()
	if issues := Validate(normalizedIn(guest, virtual)); len(issues) != 0 {
		t.Fatalf("a libvirt Machine that declares no root disk was refused: %v", issues)
	}
	hints := m("minSizeGigabytes", api.IntegerValue("0"), "model", "1e3", "vendor", "0o17", "rotational", false)
	every := map[string]api.Value{
		"hctl":         m("hints", hints.With("hctl", api.StringValue("1:0:0:0"))),
		"serialNumber": m("serialNumber", "0987654321"),
		"wwn":          m("wwn", "0x5000c500a1b2c3d4"),
		"deviceName":   m("hints", m("deviceName", "/dev/vda")),
		"model":        m("hints", hints),
	}
	for name, declared := range every {
		t.Run(name, func(t *testing.T) {
			machine, catalog := withRootDisk(guest, virtual, declared)
			want := api.Issue{Code: "api.invariant", Field: "$.spec.hardware.rootDisk",
				Message:     "a libvirt Machine declares no spec.hardware.rootDisk: the provider creates the root disk, and every installer on it writes /dev/vda",
				Remediation: "remove spec.hardware.rootDisk from Machine/guest"}
			if issues := Validate(machine, catalog); !slices.Contains(issues, want) {
				t.Fatalf("a libvirt Machine's %s root disk declaration gave %v", name, issues)
			}
			if name == "deviceName" || name == "model" {
				return
			}
			rootDisk := m("wwn", "0x5000c500a1b2c3d4", "hints", hints)
			if name == "hctl" {
				rootDisk = rootDisk.WithPath(api.StringValue("1:0:0:0"), "hints", "hctl")
			}
			if name == "serialNumber" {
				rootDisk = rootDisk.With("serialNumber", api.StringValue("0987654321"))
			}
			if machine, catalog := withRootDisk(server, physical, rootDisk); len(Validate(machine, catalog)) != 0 {
				t.Fatalf("a bare-metal Machine's %s hint was refused: %v", name, Validate(machine, catalog))
			}
		})
	}
	all := m("wwn", "0x5000c500a1b2c3d4", "serialNumber", "0987654321", "hints", hints.With("hctl", api.StringValue("1:0:0:0")))
	if machine, catalog := withRootDisk(server, physical, all); len(Validate(machine, catalog)) != 0 {
		t.Fatalf("a bare-metal Machine's hints were refused: %v", Validate(machine, catalog))
	}
}

func normalizedIn(machine api.Object, catalog api.Catalog) (api.Object, api.Catalog) {
	normalized, _ := Normalize(machine, catalog)
	return normalized, catalog
}

// installedFixture is the bare-metal fixture as a Bootwright-installed
// Machine, which is the only lifecycle that delivers its own host key.
func installedFixture() (api.Object, api.Catalog) {
	machine, catalog := fixture()
	machine = rootDeclared(machine, "", m("wwn", "0x5000c500a1b2c3d4"))
	machine = machine.WithSpec(machine.Spec().With("os", m("install", m("profileRef", "rhel", "hostKeyRef", "node-host-key"))))
	objects := []api.Object{machine, object(api.MachineInstallProfile, "rhel", m())}
	for _, existing := range catalog.Objects() {
		if existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	// Validation reads effective state, where the provider's BMC defaults and
	// the sole segment have already been inherited.
	complete := api.NewCatalog(objects)
	normalized, _ := Normalize(machine, complete)
	objects[0] = normalized
	return normalized, api.NewCatalog(objects)
}

// A physical machine offers no channel to read back what it holds, so the key
// it will answer with is declared before the installation that delivers it.
func TestBaremetalInstallationRequiresTheHostKeyItDelivers(t *testing.T) {
	machine, catalog := installedFixture()
	if issues := Validate(machine, catalog); len(issues) != 0 {
		t.Fatalf("a declared host key was refused: %v", issues)
	}
	spec := machine.Spec()
	spec = spec.With("os", spec.Get("os").With("install", spec.Get("os", "install").Without("hostKeyRef")))
	issues := Validate(machine.WithSpec(spec), catalog)
	if !mentions(issues, "$.spec.os.install.hostKeyRef") {
		t.Fatalf("a bare-metal installation without its host key was accepted: %v", issues)
	}
}

// The key belongs to the one lifecycle that delivers it. A machine its
// substrate creates and proves another way declares none.
func TestInstalledLibvirtAndBaremetalMayDeclareAHostKey(t *testing.T) {
	machine, catalog := installedFixture()
	kubevirt := object(api.InfraProvider, "metal", m("kubevirt", m("kubeconfigRef", "host", "namespace", "guests")))
	others := []api.Object{machine, kubevirt}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.InfraProvider && existing.Identity() != machine.Identity() {
			others = append(others, existing)
		}
	}
	if !mentions(Validate(machine, api.NewCatalog(others)), "$.spec.os.install.hostKeyRef") {
		t.Fatal("a KubeVirt Machine was allowed to deliver a host key")
	}
	virtual := object(api.InfraProvider, "metal", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
		"bmcEmulation", m("auth", m("credentialsRef", "bmc")))))
	objects := []api.Object{machine, virtual}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.InfraProvider && existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	if issues := Validate(machine, api.NewCatalog(objects)); mentions(issues, "$.spec.os.install.hostKeyRef") {
		t.Fatalf("an installed libvirt Machine cannot select delivered identity: %+v", issues)
	}
}

// A host key identifies exactly one machine, so two Machines sharing one would
// each satisfy the other's completion proof.
func TestAHostKeyIdentifiesOneMachine(t *testing.T) {
	machine, catalog := installedFixture()
	peer := object(api.Machine, "peer", machine.Spec())
	objects := append(catalog.Objects(), peer)
	if !mentions(Validate(machine, api.NewCatalog(objects)), "$.spec.os.install.hostKeyRef") {
		t.Fatal("two Machines were allowed to share one host key")
	}
}

func mentions(issues []api.Issue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}

// withProviderBMC is the fixture with its provider's BMC defaults replaced.
func withProviderBMC(bmc api.Value) (api.Object, api.Catalog) {
	machine, catalog := fixture()
	objects := catalog.Objects()
	for index, candidate := range objects {
		if candidate.Kind() == api.InfraProvider {
			objects[index] = candidate.WithSpec(candidate.Spec().WithPath(bmc, "baremetal", "defaults", "bmc"))
		}
	}
	return machine, api.NewCatalog(objects)
}

// A provider's bundle reaches only the Machines that keep verification. One
// that turns it off inherits no bundle and is admitted; one that keeps it
// inherits the provider's; and a Machine's own bundle always wins.
func TestAProviderBundleIsNotInheritedByAMachineThatOptsOut(t *testing.T) {
	machine, catalog := withProviderBMC(m("credentialsRef", "bmc", "tls", m("trustBundleRef", "metal-ca")))
	for name, test := range map[string]struct {
		tls  api.Value
		want string
	}{
		"opts out":       {m("verification", "none"), ""},
		"keeps":          {m("verification", "full"), "metal-ca"},
		"authors none":   {api.Value{}, "metal-ca"},
		"its own bundle": {m("trustBundleRef", "node-ca"), "node-ca"},
	} {
		t.Run(name, func(t *testing.T) {
			bmc := machine.Spec().Get("hardware", "bmc").Without("tls")
			if test.tls.Present() {
				bmc = bmc.With("tls", test.tls)
			}
			local := machine.WithSpec(machine.Spec().WithPath(bmc, "hardware", "bmc"))
			normalized, _ := Normalize(local, catalog)
			if got := normalized.Spec().Get("hardware", "bmc", "tls", "trustBundleRef").Text(); got != test.want {
				t.Fatalf("effective bundle = %q, want %q", got, test.want)
			}
			if issues := Validate(normalized, catalog); len(issues) != 0 {
				t.Fatalf("effective Machine refused: %v", issues)
			}
		})
	}
}

// A Machine kind default would hand the virtual-media exception to every
// Machine that authors no trust, so it refuses there; any other trust is an
// ordinary default.
func TestAMachineKindDefaultCannotDefaultDisableVerification(t *testing.T) {
	const field = "$.spec.hardware.bmc.virtualMedia.tls.trust"
	for trust, refused := range map[string]bool{"disableVerification": true, "importCertificate": false, "established": false} {
		partial := object(api.Machine, "", m("hardware", m("bmc", m("virtualMedia", m("tls", m("trust", trust))))))
		issues := ValidatePartial(partial, api.Catalog{})
		if mentions(issues, field) != refused {
			t.Fatalf("a %s kind default: issues %v", trust, issues)
		}
		if refused && (issues[0].Message != "disableVerification is a per-Machine exception and never a kind default" || issues[0].Code != "api.invariant") {
			t.Fatalf("issue = %+v", issues[0])
		}
	}
}

// Authored on one Machine, disableVerification is the explicit exception: it
// is admitted, replaces the provider's trust, and is visible in effective
// state with verification restored after boot unless the Machine says not to.
func TestMachineVirtualMediaDisableVerificationIsAnExplicitException(t *testing.T) {
	machine, catalog := fixture()
	machine = machine.WithSpec(machine.Spec().WithPath(m("tls", m("trust", "disableVerification")), "hardware", "bmc", "virtualMedia"))
	if issues := ValidateAuthored(machine, catalog); len(issues) != 0 {
		t.Fatalf("an authored per-Machine exception refused: %v", issues)
	}
	normalized, _ := Normalize(machine, catalog)
	tls := normalized.Spec().Get("hardware", "bmc", "virtualMedia", "tls")
	if tls.Get("trust").Text() != "disableVerification" || !tls.Get("restoreVerificationAfterBoot").Bool() || tls.Has("removeCertificateAfterBoot") {
		t.Fatalf("effective virtual-media trust = %v", tls)
	}
	if issues := Validate(normalized, catalog); len(issues) != 0 {
		t.Fatalf("the effective exception refused: %v", issues)
	}
}

// A Bootwright-installed Anaconda Machine installs over the one static IPv4
// address the Kickstart carries. DHCP installation is not supported, so a
// network that would leave the installer to lease its address, or an IPv6
// install address, refuses at admission with the change that fixes it.
func TestAnacondaInstallationRequiresAStaticIPv4Address(t *testing.T) {
	const field = "$.spec.network.installAddressRef"
	build := func(osSpec, interfaces api.Value, cidr string, addresses api.Value) (api.Object, api.Catalog) {
		machine, catalog := fixture()
		var routes, nics []api.Value
		for i, iface := range interfaces.Items() {
			routes = append(routes, m("destination", "0.0.0.0/0", "next-hop-interface", iface.Get("name").Text()))
			nics = append(nics, m("name", iface.Get("name").Text(), "macAddress", fmt.Sprintf("02:00:00:00:00:%02x", i+1)))
		}
		network := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", interfaces, "routes", m("config", list(routes...)))))
		family := "ipv4"
		if strings.Contains(cidr, ":") {
			family = "ipv6"
		}
		machine = machine.WithSpec(machine.Spec().With("os", osSpec).WithPath(addresses, "network", "addresses").WithPath(list(nics...), "hardware", "nics"))
		objects := []api.Object{machine, network, object(api.MachineInstallProfile, "rhel", m("installer", m("anaconda", m("imageRef", "image")))), object(api.Network, "lan", m("provided", true, family, m("cidr", cidr)))}
		for _, existing := range catalog.Objects() {
			if existing.Kind() != api.Machine && existing.Kind() != api.NetworkConfig && existing.Kind() != api.Network {
				objects = append(objects, existing)
			}
		}
		normalized, _ := Normalize(machine, api.NewCatalog(objects))
		objects[0] = normalized
		return normalized, api.NewCatalog(objects)
	}
	at := func(issues []api.Issue) []api.Issue {
		var found []api.Issue
		for _, issue := range issues {
			if issue.Field == field {
				found = append(found, issue)
			}
		}
		return found
	}
	installed := m("install", m("profileRef", "rhel"))
	dhcp := list(m("name", "eth0", "type", "ethernet", "ipv4", m("enabled", true, "dhcp", true)))
	contactOnly := list(m("name", "fqdn", "address", "node.example.test"))
	refused := map[string]func() (api.Object, api.Catalog){
		"a DHCP-only network": func() (api.Object, api.Catalog) { return build(installed, dhcp, "192.0.2.0/24", contactOnly) },
		"no network configuration": func() (api.Object, api.Catalog) {
			machine, catalog := fixture()
			machine = machine.WithSpec(machine.Spec().With("os", installed).With("network", m("addresses", contactOnly)))
			objects := []api.Object{machine, object(api.MachineInstallProfile, "rhel", m("installer", m("anaconda", m("imageRef", "image"))))}
			for _, existing := range catalog.Objects() {
				if existing.Kind() != api.Machine {
					objects = append(objects, existing)
				}
			}
			normalized, _ := Normalize(machine, api.NewCatalog(objects))
			objects[0] = normalized
			return normalized, api.NewCatalog(objects)
		},
		"an IPv6 install address": func() (api.Object, api.Catalog) {
			return build(installed, list(m("name", "eth0", "type", "ethernet", "ipv6", m("enabled", true))), "2001:db8::/64",
				list(m("name", "primary", "address", "2001:db8::11", "interface", "eth0")))
		},
	}
	for name, setup := range refused {
		t.Run(name, func(t *testing.T) {
			found := at(Validate(setup()))
			remedy := "assign an IPv4 address to the install interface in spec.network.addresses and select it with spec.network.installAddressRef"
			// No install interface exists before a network configuration does,
			// so its remedy selects one first.
			if name == "no network configuration" {
				remedy = "select a network configuration with spec.network.configRef or declare one in spec.network.inline, then " + remedy
			}
			if len(found) != 1 || found[0].Code != "api.invariant" ||
				found[0].Message != "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported" ||
				found[0].Remediation != remedy {
				t.Fatalf("refusal at %s = %#v, want the static IPv4 rule and its remedy", field, found)
			}
		})
	}
	admitted := map[string]func() (api.Object, api.Catalog){
		"a provided Machine on DHCP": func() (api.Object, api.Catalog) { return build(m("provided", m()), dhcp, "192.0.2.0/24", contactOnly) },
		"a cluster node on DHCP": func() (api.Object, api.Catalog) {
			return build(m("clusterInstalled", m()), dhcp, "192.0.2.0/24", contactOnly)
		},
		"a static IPv4 install address": func() (api.Object, api.Catalog) {
			return build(installed, list(m("name", "eth0", "type", "ethernet")), "192.0.2.0/24",
				list(m("name", "fqdn", "address", "node.example.test"), m("name", "primary", "address", "192.0.2.11", "interface", "eth0")))
		},
	}
	for name, setup := range admitted {
		t.Run(name, func(t *testing.T) {
			if found := at(Validate(setup())); len(found) != 0 {
				t.Fatalf("refused at %s: %#v", field, found)
			}
		})
	}
	ambiguous, catalog := build(installed, list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet")), "192.0.2.0/24",
		list(m("name", "primary", "address", "192.0.2.11", "interface", "eth0"), m("name", "secondary", "address", "192.0.2.12", "interface", "eth1")))
	if found := at(Validate(ambiguous, catalog)); len(found) != 1 || !strings.Contains(found[0].Message, "ambiguous") {
		t.Fatalf("an ambiguous install address refused %#v, want its ambiguity alone", found)
	}
}

func TestAMachineKindDefaultCannotDeclareFirmware(t *testing.T) {
	const field = "$.spec.hardware.firmware"
	partial := object(api.Machine, "", m("hardware", m("firmware", m("secureBoot", true))))
	issues := ValidatePartial(partial, api.Catalog{})
	if len(issues) != 1 || issues[0] != (api.Issue{Code: "api.invariant", Field: field,
		Message: "firmware is per-Machine intent and never a kind default", Remediation: "declare hardware.firmware on each Machine that needs it"}) {
		t.Fatalf("a firmware kind default: %+v", issues)
	}
	if issues := ValidatePartial(object(api.Machine, "", m("hardware", m("bmc", m("tls", m("verification", "full"))))), api.Catalog{}); mentions(issues, field) {
		t.Fatalf("a default without firmware was refused: %+v", issues)
	}
}

func TestAMachineKindDefaultCannotDeclareDiskRemovalOrDataDisks(t *testing.T) {
	for _, tc := range []struct{ field, message, remediation string }{
		{"$.spec.hardware.rootDisk.onRemoval", "rootDisk.onRemoval is per-Machine intent and never a kind default", "declare hardware.rootDisk.onRemoval on each Machine that needs it"},
		{"$.spec.hardware.dataDisks", "dataDisks is per-Machine intent and never a kind default", "declare hardware.dataDisks on each Machine that needs it"},
	} {
		hardware := m("rootDisk", m("onRemoval", "logicalOverwrite"))
		if tc.field == "$.spec.hardware.dataDisks" {
			hardware = m("dataDisks", list(m("serialNumber", "SER-1", "sizeBytes", api.IntegerValue("1099511627776"), "onRemoval", "logicalOverwrite")))
		}
		issues := ValidatePartial(object(api.Machine, "", m("hardware", hardware)), api.Catalog{})
		if len(issues) != 1 || issues[0] != (api.Issue{Code: "api.invariant", Field: tc.field, Message: tc.message, Remediation: tc.remediation}) {
			t.Fatalf("a %s kind default: %+v", tc.field, issues)
		}
	}
	if issues := ValidatePartial(object(api.Machine, "", m("hardware", m("rootDisk", m("hints", m("minSizeGigabytes", api.IntegerValue("100")))))), api.Catalog{}); len(issues) != 0 {
		t.Fatalf("a rootDisk default without onRemoval was refused: %+v", issues)
	}
}

func TestFirmwareOnALibvirtProviderRefusesAtItsField(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	attached := installOn("192.0.2.11")
	machine := attached.WithSpec(attached.Spec().WithPath(m("secureBoot", true), "hardware", "firmware"))
	refusal := api.Issue{Code: "api.invariant", Field: "$.spec.hardware.firmware",
		Message:     "a libvirt Machine declares no spec.hardware.firmware: the provider creates the machine's firmware, so it declares no firmware settings",
		Remediation: "remove spec.hardware.firmware from Machine/guest"}
	if issues := admission(machine, lab...); !slices.Contains(issues, refusal) {
		t.Fatalf("firmware on a libvirt provider: %#v", issues)
	}
}

// rootDeclared replaces a Machine's root-disk declaration: physical is the
// rootDisk record, deviceName its hints.deviceName. The other hints and the
// onRemoval the Machine already declares stay.
func rootDeclared(machine api.Object, deviceName string, physical api.Value) api.Object {
	current := machine.Spec().Get("hardware", "rootDisk")
	hints := current.Get("hints").Without("deviceName").Without("hctl")
	for _, hint := range physical.Get("hints").Fields() {
		hints = hints.With(hint.Name, hint.Value)
	}
	if deviceName != "" {
		hints = hints.With("deviceName", api.StringValue(deviceName))
	}
	declared := physical.Without("hints")
	if hints.Len() > 0 {
		declared = declared.With("hints", hints)
	}
	if current.Has("onRemoval") && !declared.Has("onRemoval") {
		declared = declared.With("onRemoval", current.Get("onRemoval"))
	}
	return machine.WithSpec(machine.Spec().WithPath(declared, "hardware", "rootDisk"))
}

func replaced(catalog api.Catalog, objects ...api.Object) api.Catalog {
	all := append([]api.Object{}, objects...)
	for _, existing := range catalog.Objects() {
		if !slices.ContainsFunc(objects, func(o api.Object) bool { return o.Identity() == existing.Identity() }) {
			all = append(all, existing)
		}
	}
	return api.NewCatalog(all)
}

// A Bootwright-installed bare-metal Machine may name its root disk by serial
// number and exact size alone; a serial number without its size, a size on an
// OS-ready Machine and an unaligned size refuse.
func TestABaremetalRootDiskMayBeNamedBySerialNumberAndSize(t *testing.T) {
	machine, catalog := installedFixture()
	bySerial := m("serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("480103981056"))
	serial := rootDeclared(machine, "", bySerial)
	if issues := Validate(serial, replaced(catalog, serial)); len(issues) != 0 {
		t.Fatalf("a root disk named by serial number and size was refused: %v", issues)
	}
	alone := rootDeclared(machine, "", m("serialNumber", "SYNTHETIC-ROOT-01"))
	if issues := Validate(alone, replaced(catalog, alone)); !mentions(issues, "$.spec.hardware.rootDisk") {
		t.Fatalf("a serial number without its size was admitted as a root selector: %v", issues)
	}
	unaligned := rootDeclared(machine, "", bySerial.With("sizeBytes", api.IntegerValue("480103981057")))
	issues := Validate(unaligned, replaced(catalog, unaligned))
	if len(issues) != 1 || issues[0].Field != "$.spec.hardware.rootDisk.sizeBytes" || issues[0].Remediation != "set sizeBytes to the root disk's exact logical capacity, as blockdev --getsize64 reports it" {
		t.Fatalf("issues = %+v", issues)
	}
	provisioned, provisionedCatalog := fixture()
	provisioned = rootDeclared(provisioned, "/dev/sda", bySerial)
	provided := provisioned.WithSpec(provisioned.Spec().With("os", m("provided", m())))
	for name, other := range map[string]api.Object{"OS-ready": provided} {
		issues := Validate(other, replaced(provisionedCatalog, other))
		refused := false
		for _, issue := range issues {
			if issue.Field == "$.spec.hardware.rootDisk.sizeBytes" {
				refused = issue.Code == "api.invariant" && issue.Remediation == "remove spec.hardware.rootDisk.sizeBytes"
			}
		}
		if !refused {
			t.Fatalf("%s: sizeBytes was not refused: %v", name, issues)
		}
	}
}

// A serial number that alone selects a disk is declared by no other Machine,
// because two disks reporting one serial cannot be told apart by it.
func TestASelectingSerialNumberIsDeclaredByNoOtherMachine(t *testing.T) {
	machine, catalog := installedFixture()
	serial := rootDeclared(machine, "", m("serialNumber", "SYNTHETIC-SHARED-01").With("sizeBytes", api.IntegerValue("480103981056")))
	other := object(api.Machine, "other", m("os", m("provided", m())))
	other = rootDeclared(other, "/dev/disk/by-id/wwn-0x5000000000000001", m("serialNumber", "SYNTHETIC-SHARED-01"))
	issues := validateSelectingSerials(serial, replaced(catalog, serial, other))
	if len(issues) != 1 || issues[0].Field != "$.spec.hardware.rootDisk.serialNumber" ||
		issues[0].Message != "a disk selected by its serial number needs a serial number no other Machine declares, and Machine/other declares SYNTHETIC-SHARED-01" {
		t.Fatalf("issues = %+v", issues)
	}
	byPath := rootDeclared(machine, "/dev/disk/by-id/wwn-0x5000000000000002", m("serialNumber", "SYNTHETIC-SHARED-01"))
	if issues := validateSelectingSerials(byPath, replaced(catalog, byPath, other)); len(issues) != 0 {
		t.Fatalf("a serial number beside its path was refused: %v", issues)
	}
	clear := byPath.WithSpec(byPath.Spec().WithPath(list(m("serialNumber", "SYNTHETIC-SHARED-01", "sizeBytes", api.IntegerValue("8192"), "onInstall", "metadataClear")), "hardware", "dataDisks"))
	if issues := validateSelectingSerials(clear, replaced(catalog, clear, other)); len(issues) != 1 || issues[0].Field != "$.spec.hardware.dataDisks[0].serialNumber" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestPhysicalAgentRootRequiresAStableSelector(t *testing.T) {
	original, catalog := fixture()
	original, normalization := Normalize(original, catalog)
	if len(normalization) != 0 {
		t.Fatal(normalization)
	}
	for _, test := range []struct {
		name, device, wwn, serial, size string
		allowed                         bool
	}{
		{"kernel name", "/dev/sda", "", "", "", false}, {"by path", "/dev/disk/by-path/pci-0000:00:01.0", "", "", "", false},
		{"missing selector", "", "", "", "", false}, {"invalid WWN", "", "disk-one", "", "", false},
		{"uppercase WWN", "", "0X5000C500A1B2C3D4", "", "", false}, {"stable WWN", "", "0x5000c500a1b2c3d4", "", "", true},
		{"extended WWN", "", "0x5000c500a1b2c3d45000c500a1b2c3d4", "", "", true},
		{"unstable name beside WWN", "/dev/sda", "0x5000c500a1b2c3d4", "", "", false},
		{"serial and size", "", "", "SYNTHETIC-ROOT-01", "8192", true},
		{"numeric serial", "", "", "0000123456", "8192", true},
		{"serial and WWN", "", "0x5000c500a1b2c3d4", "SYNTHETIC-ROOT-01", "", true},
		{"invalid WWN beside serial", "", "bad", "SYNTHETIC-ROOT-01", "", false},
		{"invalid serial beside WWN", "", "0x5000c500a1b2c3d4", " space ", "", false},
		{"unstable name beside serial", "/dev/sda", "", "SYNTHETIC-ROOT-01", "", false},
		{"serial without size", "", "", "SYNTHETIC-ROOT-01", "", false},
		{"unaligned size", "", "", "SYNTHETIC-ROOT-01", "8193", false},
		{"long serial", "", "", strings.Repeat("a", 257), "", false},
		{"whitespace in serial", "", "", "bad serial", "", false},
		{"newline in serial", "", "", "bad\nserial", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			physical := m()
			if test.wwn != "" {
				physical = m("wwn", test.wwn)
			}
			if test.serial != "" {
				physical = physical.With("serialNumber", api.StringValue(test.serial))
			}
			if test.size != "" {
				physical = physical.With("sizeBytes", api.IntegerValue(test.size))
			}
			candidate := rootDeclared(original, test.device, physical)
			issues := Validate(candidate, replaced(catalog, candidate))
			if (len(issues) == 0) != test.allowed {
				t.Fatalf("root selector allowed=%v: %+v", test.allowed, issues)
			}
		})
	}
}

func TestPhysicalAgentSerialAndSizeRejectsAmbiguousDeclarations(t *testing.T) {
	original, catalog := fixture()
	original, issues := Normalize(original, catalog)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	selected := rootDeclared(original, "", m("serialNumber", "SYNTHETIC-SHARED-AGENT").With("sizeBytes", api.IntegerValue("8192")))
	other := rootDeclared(object(api.Machine, "other", m("os", m("provided", m()))), "/dev/disk/by-id/wwn-0x5000000000000001", m("serialNumber", "SYNTHETIC-SHARED-AGENT"))
	if issues := Validate(selected, replaced(catalog, selected, other)); len(issues) != 1 || issues[0].Field != "$.spec.hardware.rootDisk.serialNumber" || !strings.Contains(issues[0].Remediation, "rootDisk.wwn") || strings.Contains(issues[0].Remediation, "/dev/") {
		t.Fatalf("duplicate selecting serial = %+v", issues)
	}
	sized := rootDeclared(original, "", m("wwn", "0x5000000000000001").With("sizeBytes", api.IntegerValue("8192")))
	if issues := Validate(sized, replaced(catalog, sized)); !mentions(issues, rootSizePath) {
		t.Fatalf("unrealizable exact agent size = %+v", issues)
	}
}

// The verification policy preserves Machine-local precedence over a provider's
// bundle: a Machine that turns verification off inherits no bundle, and one that
// authors its own bundle keeps it.
func TestBMCTLSVerificationPolicyPreservesLocalOverrides(t *testing.T) {
	machine, catalog := withProviderBMC(m("credentialsRef", "bmc", "tls", m("trustBundleRef", "metal-ca")))
	for _, test := range []struct {
		tls            api.Value
		policy, bundle string
	}{
		{m("verification", "none"), "none", ""},
		{m("verification", "certificate", "trustBundleRef", "node-trust"), "certificate", "node-trust"},
		{m("verification", "full"), "full", "metal-ca"},
		{m(), "", "metal-ca"},
	} {
		local := machine.WithSpec(machine.Spec().WithPath(test.tls, "hardware", "bmc", "tls"))
		normalized, _ := Normalize(local, catalog)
		got := normalized.Spec().Get("hardware", "bmc", "tls")
		if got.Has("verify") || got.Get("verification").Text() != test.policy || got.Get("trustBundleRef").Text() != test.bundle {
			t.Fatalf("local TLS override lost: %v", got)
		}
	}
}

func TestAutomaticRootDiscoveryRequiresExclusivePolicyAndVerifiedPhysicalInstallation(t *testing.T) {
	target, catalog := installedFixture()
	target = rootDeclared(target, "", m("discover", m()))
	target = target.WithSpec(target.Spec().WithPath(m("verification", "full"), "hardware", "bmc", "tls"))
	if issues := Validate(target, replaced(catalog, target)); len(issues) != 0 {
		t.Fatalf("automatic policy refused: %+v", issues)
	}
	changes := []api.Object{
		rootDeclared(target, "", m("discover", m(), "bootVolume", m())),
		target.WithSpec(target.Spec().With("os", m("provided", m()))),
		target.WithSpec(target.Spec().WithPath(m("verification", "none"), "hardware", "bmc", "tls")),
		rootDeclared(target, "", m("discover", m("match", m("controllerPCIAddress", "unknown")))),
		rootDeclared(target, "", m("discover", m("match", m("minSizeBytes", api.IntegerValue("8192"), "maxSizeBytes", api.IntegerValue("4096"))))),
	}
	for _, changed := range changes {
		if issues := Validate(changed, replaced(catalog, changed)); !mentions(issues, "$.spec.hardware.rootDisk.discover") {
			t.Fatalf("invalid discovery admitted: %+v", issues)
		}
	}
}
