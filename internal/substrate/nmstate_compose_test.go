package substrate

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func ethernet(name string, extra ...api.FieldValue) api.Value {
	return api.MapValue(append([]api.FieldValue{text("name", name), text("type", "ethernet")}, extra...)...)
}

func networkConfig(nmstate api.Value) api.Object {
	return api.NewObject(api.NetworkConfig, "net", api.Value{}, api.MapValue(field("nmstate", nmstate)))
}

func machineWithOverrides(overrides api.Value) api.Object {
	return api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(
		field("network", api.MapValue(text("configRef", "net"), field("overrides", overrides))),
	))
}

func routesOf(routes ...api.Value) api.Value {
	return api.MapValue(field("config", api.ListValue(routes...)))
}

func route(fields ...api.FieldValue) api.Value { return api.MapValue(fields...) }

// Realization and installation read the network the Machine composes, so an
// override that removes, adds or reroutes reaches both.
func TestTheNetworkTemplateCarriesTheMachinesOverrides(t *testing.T) {
	config := networkConfig(api.MapValue(
		field("interfaces", api.ListValue(ethernet("eth0"), ethernet("eth1"))),
		field("routes", routesOf(route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1")))),
	))
	machine := machineWithOverrides(api.MapValue(
		field("interfaces", api.ListValue(api.MapValue(text("name", "eth1"), text("state", "absent")), ethernet("eth2"))),
		field("routes", routesOf(route(text("next-hop-address", "198.51.100.254")))),
	))
	catalog := api.NewCatalog([]api.Object{config, machine})
	template, err := NetworkTemplate(catalog, machine)
	if err != nil {
		t.Fatal(err)
	}
	interfaces := template.Get("interfaces").Items()
	if len(interfaces) != 3 || interfaces[1].Get("name").Text() != "eth1" || interfaces[1].Get("state").Text() != "absent" || interfaces[2].Get("name").Text() != "eth2" {
		t.Fatalf("interfaces = %v", template.Get("interfaces"))
	}
	names, err := EthernetInterfaces(catalog, machine)
	if err != nil || !slices.Equal(names, []string{"eth0", "eth2"}) {
		t.Fatalf("ethernet interfaces = %v (%v)", names, err)
	}
	if gateway := DefaultGateway(template); gateway != "198.51.100.254" {
		t.Fatalf("gateway = %q", gateway)
	}
}

// The examples declare an empty override, which must compose to the template
// itself.
func TestAnEmptyOverrideLeavesTheTemplateUnchanged(t *testing.T) {
	nmstate := api.MapValue(
		field("interfaces", api.ListValue(ethernet("eth0", number("mtu", "1500")), ethernet("eth1"))),
		field("routes", routesOf(route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1")))),
	)
	machine := machineWithOverrides(api.MapValue())
	template, err := NetworkTemplate(api.NewCatalog([]api.Object{networkConfig(nmstate), machine}), machine)
	if err != nil {
		t.Fatal(err)
	}
	if !template.Equal(nmstate) {
		t.Fatalf("template = %v, want %v", template, nmstate)
	}
}

// An override that cannot merge is refused rather than silently replacing the
// template, and the refusal never echoes what the override declares.
func TestOverridesThatCannotMergeRefuse(t *testing.T) {
	config := networkConfig(api.MapValue(field("interfaces", api.ListValue(ethernet("eth0")))))
	machine := machineWithOverrides(api.MapValue(field("interfaces", api.ListValue(
		api.MapValue(text("name", "eth0")), api.MapValue(text("type", "ethernet")),
	))))
	_, err := NetworkTemplate(api.NewCatalog([]api.Object{config, machine}), machine)
	found := diagnostics.Of(err)
	if len(found) != 1 {
		t.Fatalf("err = %v", err)
	}
	refused := found[0]
	if refused.Code != "api.invariant" || refused.Message != "the Machine's network overrides do not merge into its network template" ||
		refused.Remediation != "make each list in spec.network.overrides on Machine/guest and the list it merges into uniformly named or uniformly unnamed maps" {
		t.Fatalf("refusal = %+v", refused)
	}
	if strings.Contains(refused.Message+refused.Remediation, "eth0") {
		t.Fatalf("refusal echoes the override: %+v", refused)
	}
}

func TestMergeNativeFollowsTheMergeContract(t *testing.T) {
	named := func(name, key, value string) api.Value { return api.MapValue(text("name", name), text(key, value)) }
	for name, test := range map[string]struct {
		base, override, want api.Value
		ok                   bool
	}{
		"named lists merge by name and append": {
			api.ListValue(named("a", "x", "1"), named("b", "x", "2")),
			api.ListValue(named("b", "y", "3"), named("c", "x", "4")),
			api.ListValue(named("a", "x", "1"), api.MapValue(text("name", "b"), text("x", "2"), text("y", "3")), named("c", "x", "4")),
			true,
		},
		"positional lists merge by index": {
			api.ListValue(api.MapValue(text("x", "1")), api.MapValue(text("x", "2"))),
			api.ListValue(api.MapValue(text("y", "3"))),
			api.ListValue(api.MapValue(text("x", "1"), text("y", "3")), api.MapValue(text("x", "2"))),
			true,
		},
		"a scalar list cannot merge": {
			api.ListValue(api.StringValue("a")), api.ListValue(api.StringValue("b")), api.Value{}, false,
		},
		"a scalar override wins over a mapping": {
			api.MapValue(text("x", "1")), api.StringValue("replaced"), api.StringValue("replaced"), true,
		},
		"a nested list that cannot merge refuses the whole merge": {
			api.MapValue(field("outer", api.MapValue(field("items", api.ListValue(named("a", "x", "1")))))),
			api.MapValue(field("outer", api.MapValue(field("items", api.ListValue(api.MapValue(text("x", "2"))))))),
			api.Value{}, false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			merged, ok := MergeNative(test.base, test.override)
			if ok != test.ok || (ok && !merged.Equal(test.want)) {
				t.Fatalf("merged = %v (%t), want %v (%t)", merged, ok, test.want, test.ok)
			}
		})
	}
}

// A domain attaches only interfaces the composed network keeps available.
func TestAbsentAndIgnoredEthernetInterfacesAreNotRealized(t *testing.T) {
	machine := machineWithNetwork("net")
	config := networkConfig(api.MapValue(field("interfaces", api.ListValue(
		ethernet("eth0"), ethernet("eth1", text("state", "absent")), ethernet("eth2", text("state", "ignore")),
		api.MapValue(text("name", "bond0"), text("type", "bond")),
	))))
	names, err := EthernetInterfaces(api.NewCatalog([]api.Object{config, machine}), machine)
	if err != nil || !slices.Equal(names, []string{"eth0"}) {
		t.Fatalf("ethernet interfaces = %v (%v)", names, err)
	}
	unavailable := networkConfig(api.MapValue(field("interfaces", api.ListValue(
		ethernet("eth1", text("state", "absent")), ethernet("eth2", text("state", "ignore")),
	))))
	_, err = EthernetInterfaces(api.NewCatalog([]api.Object{unavailable, machine}), machine)
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Message != "the Machine's network template declares no ethernet interface that is not absent or ignored" {
		t.Fatalf("err = %+v", found)
	}
}

func TestTheDefaultGatewayIsTheFirstNonAbsentIPv4DefaultRouteWithAnIPv4NextHop(t *testing.T) {
	absent := text("state", "absent")
	for name, test := range map[string]struct {
		routes []api.Value
		want   string
	}{
		"an absent default route is skipped": {[]api.Value{
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), absent),
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.2")),
		}, "198.51.100.2"},
		"an IPv6 default route is skipped": {[]api.Value{
			route(text("destination", "::/0"), text("next-hop-address", "2001:db8::1")),
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.3")),
		}, "198.51.100.3"},
		"a default route without a next hop is skipped": {[]api.Value{
			route(text("destination", "0.0.0.0/0")),
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.4")),
		}, "198.51.100.4"},
		"an IPv4 default route with an IPv6 next hop is skipped": {[]api.Value{
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "::ffff:198.51.100.6")),
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.7")),
		}, "198.51.100.7"},
		"any zero-prefix IPv4 destination is a default route": {[]api.Value{
			route(text("destination", "192.0.2.0/0"), text("next-hop-address", "198.51.100.5")),
		}, "198.51.100.5"},
		"a narrower route is no default route": {[]api.Value{
			route(text("destination", "198.51.100.0/24"), text("next-hop-address", "198.51.100.9")),
		}, ""},
		"only an absent default route installs none": {[]api.Value{
			route(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), absent),
		}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			template := api.MapValue(field("routes", routesOf(test.routes...)))
			if gateway := DefaultGateway(template); gateway != test.want {
				t.Fatalf("gateway = %q, want %q", gateway, test.want)
			}
		})
	}
}

// The default route of an install and the gateway composed for it read a
// destination one way: any IPv4 CIDR with a zero prefix length.
func TestADefaultRouteIPv4IsAnyZeroPrefixIPv4Destination(t *testing.T) {
	for destination, want := range map[string]bool{
		"0.0.0.0/0": true, "192.0.2.0/0": true, "10.1.2.3/0": true,
		"::/0": false, "0.0.0.0/1": false, "198.51.100.0/24": false, "garbage": false, "": false,
	} {
		if got := DefaultRouteIPv4(destination); got != want {
			t.Errorf("DefaultRouteIPv4(%q) = %v, want %v", destination, got, want)
		}
	}
}
