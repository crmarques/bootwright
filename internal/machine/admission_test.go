package machine

import (
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
	machine := object(api.Machine, "node", m("substrate", m("providerRef", "metal"), "os", m("provided", false, "install", m("rootDeviceHints", m("deviceName", "/dev/sda"))), "hardware", m("nics", list(m("name", "eth0", "macAddress", "02-00-00-00-00-01")), "boot", m("nicRef", "eth0"), "management", m("bmc", m("address", "redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/1"))), "network", m("configRef", "net", "addresses", list(m("name", "primary", "address", "192.0.2.11/24", "interface", "eth0")))))
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
	if validBMC("https://bmc.example.test/redfish/v1/Systems/", true) || validBMC("https://bmc.example.test/redfish/v1/Systems/1/Actions", true) {
		t.Fatal("non-exact ComputerSystem accepted")
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

func TestBMCComputerSystemRejectsDotAndWhitespaceIdentifiers(t *testing.T) {
	for _, identifier := range []string{".", "..", "a b", "a\tb", "%2e", "a%20b"} {
		if validBMC("https://bmc.example.test/redfish/v1/Systems/"+identifier, true) {
			t.Fatalf("ambiguous ComputerSystem identifier accepted: %q", identifier)
		}
	}
	if !validBMC("redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/node-1", true) {
		t.Fatal("exact ComputerSystem rejected")
	}
}

func TestNativeMergeDiagnosticsDoNotExposeMapKeys(t *testing.T) {
	key := strings.Repeat("synthetic-native-key-", 3000)
	_, issues := mergeNative(m(key, list(api.BoolValue(true))), m(key, list(api.BoolValue(false))), "$.spec.network.overrides")
	if len(issues) != 1 || issues[0].Field != "$.spec.network.overrides" || strings.Contains(issues[0].Message, key) {
		t.Fatalf("native key exposed: count%d", len(issues))
	}
}
