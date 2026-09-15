package substrate

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func number(name, value string) api.FieldValue { return field(name, api.IntegerValue(value)) }

// One declaration always realizes the same virtual machine, so a replay finds
// what it created rather than a second machine beside it.
func TestDerivedIdentitiesAreStableAndDistinct(t *testing.T) {
	first := DomainUUID("lab", "rhel-01")
	if first != DomainUUID("lab", "rhel-01") {
		t.Fatal("one declaration derived two identities")
	}
	if first == DomainUUID("other", "rhel-01") || first == DomainUUID("lab", "rhel-02") {
		t.Fatal("two declarations derived one identity")
	}
	if len(first) != 36 || strings.Count(first, "-") != 4 {
		t.Fatalf("uuid = %q", first)
	}
	// The version and variant nibbles are what make the value a UUID rather
	// than arbitrary hex a consumer might reject.
	if first[14] != '8' || !strings.ContainsRune("89ab", rune(first[19])) {
		t.Fatalf("uuid is not a well-formed version 8 value: %q", first)
	}
}

// A derived address keeps QEMU's own locally administered prefix, so it can
// never collide with a real adapter on the same segment.
func TestDerivedAddressesStayLocallyAdministered(t *testing.T) {
	address := InterfaceMAC("lab", "rhel-01", "enp1s0")
	if !strings.HasPrefix(address, "52:54:00:") || len(address) != 17 {
		t.Fatalf("mac = %q", address)
	}
	if address == InterfaceMAC("lab", "rhel-01", "enp2s0") || address == InterfaceMAC("lab", "rhel-02", "enp1s0") {
		t.Fatal("two interfaces derived one address")
	}
	if address != InterfaceMAC("lab", "rhel-01", "enp1s0") {
		t.Fatal("one interface derived two addresses")
	}
}

// Every owned name carries the context, so two contexts on one host never name
// the same object.
func TestOwnedNamesCarryTheirContext(t *testing.T) {
	for name, pair := range map[string][2]string{
		"network": {NetworkName("lab", "guests"), NetworkName("other", "guests")},
		"pool":    {PoolName("lab", "p"), PoolName("other", "p")},
		"domain":  {DomainName("lab", "m"), DomainName("other", "m")},
		"unit":    {ControllerUnitName("lab", "m"), ControllerUnitName("other", "m")},
		"disks":   {DiskDirectory("lab", "m"), DiskDirectory("other", "m")},
	} {
		t.Run(name, func(t *testing.T) {
			if pair[0] == pair[1] {
				t.Fatalf("two contexts share %q", pair[0])
			}
			if !strings.Contains(pair[0], "lab") {
				t.Fatalf("%q does not carry its context", pair[0])
			}
		})
	}
}

func providerWithPort(port string) api.Object {
	return api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(
			field("bmcEmulationDefaults", api.MapValue(number("port", port))),
		)),
	))
}

func hosted(name string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
	))
}

// Each Machine's controller port comes from the provider's base port and that
// Machine's position in the provider's own canonical order.
func TestControllerPortsAllocateInCanonicalOrder(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{providerWithPort("8000"), hosted("beta"), hosted("alpha"), hosted("gamma")})
	provider := providerWithPort("8000")
	for name, want := range map[string]int{"alpha": 8000, "beta": 8001, "gamma": 8002} {
		port, ok := ControllerPort(catalog, provider, name)
		if !ok || port != want {
			t.Fatalf("%s = %d (%t), want %d", name, port, ok, want)
		}
	}
	if _, ok := ControllerPort(catalog, provider, "absent"); ok {
		t.Fatal("a Machine the provider does not host was allocated a port")
	}
	high := api.NewCatalog([]api.Object{providerWithPort("65535"), hosted("alpha"), hosted("beta")})
	if _, ok := ControllerPort(high, providerWithPort("65535"), "beta"); ok {
		t.Fatal("an allocation past the port space was accepted")
	}
}

// A provided Machine is never realized, so it takes no controller port and
// never shifts the allocation of the Machines that are.
func TestProvidedMachinesTakeNoControllerPort(t *testing.T) {
	provided := api.NewObject(api.Machine, "alpha", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab"))),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
	))
	catalog := api.NewCatalog([]api.Object{providerWithPort("8000"), provided, hosted("beta")})
	if hosts := HostedMachines(catalog, "lab"); len(hosts) != 1 || hosts[0].Name() != "beta" {
		t.Fatalf("hosted = %v", hosts)
	}
	port, ok := ControllerPort(catalog, providerWithPort("8000"), "beta")
	if !ok || port != 8000 {
		t.Fatalf("beta = %d (%t)", port, ok)
	}
}

func machineWithNetwork(configRef string) api.Object {
	return api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(
		field("network", api.MapValue(text("configRef", configRef))),
	))
}

func TestTheDefaultGatewayComesFromTheTemplateOrNowhere(t *testing.T) {
	config := api.NewObject(api.NetworkConfig, "net", api.Value{}, api.MapValue(
		field("nmstate", api.MapValue(field("routes", api.MapValue(field("config", api.ListValue(
			api.MapValue(text("destination", "198.51.100.0/24"), text("next-hop-address", "198.51.100.9")),
			api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1")),
		)))))),
	))
	catalog := api.NewCatalog([]api.Object{config, machineWithNetwork("net")})
	template, err := NetworkTemplate(catalog, machineWithNetwork("net"))
	if err != nil {
		t.Fatal(err)
	}
	if gateway := DefaultGateway(template); gateway != "198.51.100.1" {
		t.Fatalf("gateway = %q", gateway)
	}
	empty := api.NewObject(api.NetworkConfig, "bare", api.Value{}, api.MapValue(field("nmstate", api.MapValue())))
	bare, err := NetworkTemplate(api.NewCatalog([]api.Object{empty, machineWithNetwork("bare")}), machineWithNetwork("bare"))
	if err != nil {
		t.Fatal(err)
	}
	if gateway := DefaultGateway(bare); gateway != "" {
		t.Fatalf("a template without a default route invented %q", gateway)
	}
	if _, err := NetworkTemplate(api.NewCatalog(nil), machineWithNetwork("absent")); err == nil {
		t.Fatal("an unresolved network configuration was accepted")
	}
}

// An inline configuration is the same value from the Machine's own declaration,
// so realization and installation read one shape either way.
func TestAnInlineConfigurationResolvesWithoutAReference(t *testing.T) {
	machine := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(
		field("network", api.MapValue(field("inline", api.MapValue(
			field("nmstate", api.MapValue(field("interfaces", api.ListValue(api.MapValue(text("name", "eth0")))))),
		)))),
	))
	template, err := NetworkTemplate(api.NewCatalog([]api.Object{machine}), machine)
	if err != nil {
		t.Fatal(err)
	}
	if template.Get("interfaces").Items()[0].Get("name").Text() != "eth0" {
		t.Fatalf("inline template = %v", template)
	}
}

func TestSafeSegmentRefusesWhatCannotNameAHostObject(t *testing.T) {
	for _, value := range []string{"", "-lead", "trail-", "Upper", "with space", "with/slash", strings.Repeat("a", 64)} {
		if SafeSegment(value) {
			t.Fatalf("%q was accepted as a host identifier", value)
		}
	}
	for _, value := range []string{"a", "rhel-01", "lab-guests", strings.Repeat("a", 63)} {
		if !SafeSegment(value) {
			t.Fatalf("%q was refused", value)
		}
	}
}
