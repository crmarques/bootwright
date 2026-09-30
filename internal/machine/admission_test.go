package machine

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
	provider := object(api.InfraProvider, "metal", m("baremetal", m("defaults", m("bmc", m("credentialsRef", "bmc", "tls", m("verify", false), "virtualMedia", m("tls", m("trust", "established"))))), "networkAttachments", list(m("name", "net", "baremetal", m()))))
	network := object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet")), "routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0"))))))
	machine := object(api.Machine, "node", m("substrate", m("providerRef", "metal"), "os", m("provided", false, "install", m("rootDeviceHints", m("deviceName", "/dev/sda"))), "hardware", m("nics", list(m("name", "eth0", "macAddress", "02-00-00-00-00-01")), "boot", m("nicRef", "eth0"), "management", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1"))), "network", m("configRef", "net", "addresses", list(m("name", "primary", "address", "192.0.2.11/24", "interface", "eth0")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	return machine, api.NewCatalog([]api.Object{machine, network, provider, env})
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
	if normalized.Spec().Get("hardware", "management", "bmc", "tls", "verify").Bool() {
		t.Fatal("provider TLS choice lost")
	}
	if normalized.Spec().Get("network", "attachmentRef").Text() != "net" || normalized.Spec().Get("network", "installAddressRef").Text() != "primary" {
		t.Fatal("network defaults missing")
	}
	if !normalized.Spec().Get("network", "interfaceBinding").Present() {
		t.Fatal("binding not retained")
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
	local := o.WithSpec(o.Spec().WithPath(m("tls", m("trust", "import-certificate")), "hardware", "management", "bmc", "virtualMedia"))
	local, _ = Normalize(local, c)
	tls := local.Spec().Get("hardware", "management", "bmc", "virtualMedia", "tls")
	if tls.Get("trust").Text() != "import-certificate" || tls.Has("restoreVerificationAfterBoot") {
		t.Fatal("machine virtual-media block did not replace provider block")
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
	configuration = configuration.WithSpec(configuration.Spec().WithPath(list(m("name", "eth0", "type", "ethernet"), m("name", "eth1", "type", "ethernet")), "nmstate", "interfaces"))
	o = o.WithSpec(o.Spec().Without("hardware").Without("substrate").WithPath(list(m("name", "second", "address", "192.0.2.22/24", "interface", "eth1"), m("name", "first", "address", "192.0.2.11/24", "interface", "eth0")), "network", "addresses"))
	c = api.NewCatalog([]api.Object{configuration})
	selected, issues := InstallAddress(o, c)
	if len(issues) != 0 || selected.Get("name").Text() != "first" {
		t.Fatal(selected, issues)
	}
	configuration = configuration.WithSpec(configuration.Spec().WithPath(m(), "nmstate", "routes"))
	c = api.NewCatalog([]api.Object{configuration})
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
	ready := object(api.Machine, "ready", m("os", m("provided", true)))
	ready, _ = Normalize(ready, c)
	if !ready.Spec().Has("access", "ssh", "auth", "operatorIdentity") || ready.Spec().Get("access", "ssh", "addressRef").Text() != "fqdn" {
		t.Fatal("operator defaults missing")
	}
	if got := Validate(ready, c); len(got) != 0 {
		t.Fatal(got)
	}
	for name, changed := range map[string]api.Object{
		"installed authored access": o.WithSpec(o.Spec().WithPath(api.StringValue("install"), "os", "installProfileRef").With("access", m("local", true))),
		"password without user":     ready.WithSpec(ready.Spec().WithPath(m("passwordRef", "password"), "access", "ssh", "auth")),
		"empty media TLS":           o.WithSpec(o.Spec().WithPath(m(), "hardware", "management", "bmc", "virtualMedia", "tls")),
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

func TestNetworkConfigurationCanonicalUniqueness(t *testing.T) {
	n := object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.1/24"), m("cidr", "192.0.2.2/24")), "nmstate", m()))
	n, _ = Normalize(n, api.Catalog{})
	if n.Spec().Get("machineNetwork").Items()[0].Get("cidr").Text() != "192.0.2.0/24" {
		t.Fatal("CIDR not masked")
	}
	if len(Validate(n, api.Catalog{})) == 0 {
		t.Fatal("equivalent CIDRs admitted")
	}
}

// Every authored BMC address is held to the one grammar the realized target
// reads, whether or not anything installs the Machine, and one address raises
// one issue.
func TestEveryAuthoredBMCAddressNamesOneExactSystem(t *testing.T) {
	const field = "$.spec.hardware.management.bmc.address"
	installed, catalog := fixture()
	provided := object(api.Machine, "bastion", m("os", m("provided", true),
		"hardware", m("management", m("bmc", m("address", "https://bastion-bmc.example.test/redfish/v1/Systems/1", "credentialsRef", "bmc")))))
	for name, machine := range map[string]api.Object{"bare-metal installation": installed, "provider-less": provided} {
		with := func(address string) []api.Issue {
			edited := machine.WithSpec(machine.Spec().WithPath(api.StringValue(address), "hardware", "management", "bmc", "address"))
			normalized, _ := Normalize(edited, catalog)
			return Validate(normalized, catalog)
		}
		t.Run(name, func(t *testing.T) {
			if issues := with(machine.Spec().Get("hardware", "management", "bmc", "address").Text()); len(issues) != 0 {
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

func TestManagedLibvirtAttachmentContainsTheInstallAddress(t *testing.T) {
	host := object(api.Machine, "host", m("capabilities", api.StringList("libvirt"), "os", m("provided", true)))
	provider := object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small"))), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "virbr-lab", "management", "managed", "address", "192.0.2.1/24")))))
	network := object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.0/24"), m("cidr", "198.51.100.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	for address, valid := range map[string]bool{"192.0.2.11/24": true, "198.51.100.11/24": false} {
		guest := object(api.Machine, "guest", m("substrate", m("providerRef", "lab", "profileRef", "small"), "os", m("provided", false), "network", m("configRef", "net", "attachmentRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", address, "interface", "enp1s0")))))
		c := api.NewCatalog([]api.Object{guest, host, provider, network, env})
		guest, _ = Normalize(guest, c)
		issues := Validate(guest, c)
		if (len(issues) == 0) != valid {
			t.Fatalf("%s: issues = %v", address, issues)
		}
	}
}

// withRootDeviceHints is a Machine of the catalog, normalized, with its
// root-device hints replaced.
func withRootDeviceHints(machine api.Object, catalog api.Catalog, hints api.Value) (api.Object, api.Catalog) {
	machine = machine.WithSpec(machine.Spec().WithPath(hints, "os", "install", "rootDeviceHints"))
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

// The disks a libvirt domain presents carry no WWN, SCSI address or serial
// number, so a hint on any of them could match no disk and refuses at its own
// path, while the other hints stay admitted. A physical machine's own disks
// may carry every one of them, so bare metal keeps them all.
func TestALibvirtMachineRefusesTheHintsItsDisksCannotMatch(t *testing.T) {
	host := object(api.Machine, "host", m("capabilities", api.StringList("libvirt"), "os", m("provided", true)))
	provider := object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", m("auth", m("credentialsRef", "bmc")), "machineProfiles", list(m("name", "small"))), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "virbr-lab", "management", "managed", "address", "192.0.2.1/24")))))
	network := object(api.NetworkConfig, "net", m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet")))))
	env := object(api.Environment, "env", m("domains", m("base", "example.test")))
	guest := object(api.Machine, "guest", m("substrate", m("providerRef", "lab", "profileRef", "small"), "os", m("provided", false), "network", m("configRef", "net", "attachmentRef", "net", "installAddressRef", "ip", "addresses", list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0")))))
	virtual := api.NewCatalog([]api.Object{guest, host, provider, network, env})
	server, physical := fixture()
	admitted := []api.FieldValue{
		{Name: "deviceName", Value: api.StringValue("/dev/vda")}, {Name: "minSizeGigabytes", Value: api.IntegerValue("0")},
		{Name: "model", Value: api.StringValue("1e3")}, {Name: "vendor", Value: api.StringValue("0o17")},
		{Name: "rotational", Value: api.BoolValue(false)},
	}
	if machine, catalog := withRootDeviceHints(guest, virtual, api.MapValue(admitted...)); len(Validate(machine, catalog)) != 0 {
		t.Fatalf("a libvirt Machine's admitted hints were refused: %v", Validate(machine, catalog))
	}
	const reason = "the disks this substrate creates carry no WWN, SCSI address or serial number, so this hint can match none of them"
	every := slices.Clone(admitted)
	for hint, value := range map[string]string{"hctl": "1:0:0:0", "serialNumber": "0987654321", "wwn": "0x5000c500a1b2c3d4"} {
		every = append(every, api.FieldValue{Name: hint, Value: api.StringValue(value)})
		t.Run(hint, func(t *testing.T) {
			declared := api.MapValue(append(slices.Clone(admitted), api.FieldValue{Name: hint, Value: api.StringValue(value)})...)
			machine, catalog := withRootDeviceHints(guest, virtual, declared)
			issues := Validate(machine, catalog)
			if len(issues) != 1 || issues[0].Code != "api.invariant" ||
				issues[0].Field != "$.spec.os.install.rootDeviceHints."+hint || issues[0].Message != reason {
				t.Fatalf("a libvirt Machine's %s hint gave %v", hint, issues)
			}
			if machine, catalog := withRootDeviceHints(server, physical, declared); len(Validate(machine, catalog)) != 0 {
				t.Fatalf("a bare-metal Machine's %s hint was refused: %v", hint, Validate(machine, catalog))
			}
		})
	}
	if machine, catalog := withRootDeviceHints(server, physical, api.MapValue(every...)); len(Validate(machine, catalog)) != 0 {
		t.Fatalf("a bare-metal Machine's hints were refused: %v", Validate(machine, catalog))
	}
}

// installedFixture is the bare-metal fixture as a Bootwright-installed
// Machine, which is the only lifecycle that delivers its own host key.
func installedFixture() (api.Object, api.Catalog) {
	machine, catalog := fixture()
	spec := machine.Spec()
	install := spec.Get("os", "install").With("hostKeyRef", api.StringValue("node-host-key"))
	spec = spec.With("os", spec.Get("os").
		With("installProfileRef", api.StringValue("rhel")).
		With("install", install))
	machine = machine.WithSpec(spec)
	objects := []api.Object{machine, object(api.MachineInstallProfile, "rhel", m())}
	for _, existing := range catalog.Objects() {
		if existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	// Validation reads effective state, where the provider's BMC defaults and
	// the sole attachment have already been inherited.
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

// The key belongs to the one lifecycle that delivers it. A machine nothing
// installs, and one its substrate creates and proves another way, declare none.
func TestOnlyABaremetalInstallationDeclaresAHostKey(t *testing.T) {
	machine, catalog := installedFixture()
	spec := machine.Spec()
	provided := machine.WithSpec(spec.With("os", m("provided", true, "install", m("hostKeyRef", "node-host-key"))))
	if !mentions(Validate(provided, catalog), "$.spec.os.install.hostKeyRef") {
		t.Fatal("an OS-ready Machine was allowed to deliver a host key")
	}
	virtual := object(api.InfraProvider, "metal", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
		"bmcEmulationDefaults", m("auth", m("credentialsRef", "bmc")))))
	objects := []api.Object{machine, virtual}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.InfraProvider && existing.Identity() != machine.Identity() {
			objects = append(objects, existing)
		}
	}
	if !mentions(Validate(machine, api.NewCatalog(objects)), "$.spec.os.install.hostKeyRef") {
		t.Fatal("a Machine proved through its hypervisor was allowed to deliver a host key")
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

const bundleField = "$.spec.hardware.management.bmc.tls.trustBundleRef"

// A Machine may author its own bundle, but not beneath a provider that turns
// verification off: the authored block is consistent on its own, and the
// effective one, which inherited the opt-out, refuses.
func TestAMachineBundleWithAnInheritedOptOutRefuses(t *testing.T) {
	machine, catalog := fixture()
	machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("node-ca"), "hardware", "management", "bmc", "tls", "trustBundleRef"))
	if mentions(ValidateAuthored(machine, catalog), bundleField) {
		t.Fatal("an authored bundle without an authored opt-out refused")
	}
	normalized, _ := Normalize(machine, catalog)
	if normalized.Spec().Get("hardware", "management", "bmc", "tls", "verify").Bool() {
		t.Fatal("the fixture provider's opt-out was not inherited")
	}
	if !mentions(Validate(normalized, catalog), bundleField) {
		t.Fatal("a bundle beside an inherited opt-out was admitted")
	}
}

// A provider's bundle reaches only the Machines that keep verification. One
// that turns it off inherits no bundle and is admitted; one that keeps it
// inherits the provider's; and a Machine's own bundle always wins.
func TestAProviderBundleIsNotInheritedByAMachineThatOptsOut(t *testing.T) {
	machine, catalog := withProviderBMC(m("credentialsRef", "bmc", "tls", m("verify", true, "trustBundleRef", "metal-ca")))
	for name, test := range map[string]struct {
		tls  api.Value
		want string
	}{
		"opts out":       {m("verify", false), ""},
		"keeps":          {m("verify", true), "metal-ca"},
		"authors none":   {api.Value{}, "metal-ca"},
		"its own bundle": {m("trustBundleRef", "node-ca"), "node-ca"},
	} {
		t.Run(name, func(t *testing.T) {
			local := machine
			if test.tls.Present() {
				local = machine.WithSpec(machine.Spec().WithPath(test.tls, "hardware", "management", "bmc", "tls"))
			}
			normalized, _ := Normalize(local, catalog)
			if got := normalized.Spec().Get("hardware", "management", "bmc", "tls", "trustBundleRef").Text(); got != test.want {
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
	const field = "$.spec.hardware.management.bmc.virtualMedia.tls.trust"
	for trust, refused := range map[string]bool{"disable-verification": true, "import-certificate": false, "established": false} {
		partial := object(api.Machine, "", m("hardware", m("management", m("bmc", m("virtualMedia", m("tls", m("trust", trust)))))))
		issues := ValidatePartial(partial, api.Catalog{})
		if mentions(issues, field) != refused {
			t.Fatalf("a %s kind default: issues %v", trust, issues)
		}
		if refused && (issues[0].Message != "disable-verification is a per-Machine exception and never a kind default" || issues[0].Code != "api.invariant") {
			t.Fatalf("issue = %+v", issues[0])
		}
	}
}

// Authored on one Machine, disable-verification is the explicit exception: it
// is admitted, replaces the provider's trust, and is visible in effective
// state with verification restored after boot unless the Machine says not to.
func TestMachineVirtualMediaDisableVerificationIsAnExplicitException(t *testing.T) {
	machine, catalog := fixture()
	machine = machine.WithSpec(machine.Spec().WithPath(m("tls", m("trust", "disable-verification")), "hardware", "management", "bmc", "virtualMedia"))
	if issues := ValidateAuthored(machine, catalog); len(issues) != 0 {
		t.Fatalf("an authored per-Machine exception refused: %v", issues)
	}
	normalized, _ := Normalize(machine, catalog)
	tls := normalized.Spec().Get("hardware", "management", "bmc", "virtualMedia", "tls")
	if tls.Get("trust").Text() != "disable-verification" || !tls.Get("restoreVerificationAfterBoot").Bool() || tls.Has("removeCertificateAfterBoot") {
		t.Fatalf("effective virtual-media trust = %v", tls)
	}
	if issues := Validate(normalized, catalog); len(issues) != 0 {
		t.Fatalf("the effective exception refused: %v", issues)
	}
}
