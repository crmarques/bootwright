package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// withNative replaces the fixture's network template with interfaces and
// routes, renames its one interface, NIC and address to name, and returns the
// Machine and its catalog.
func withNative(name string, route api.Value) (api.Object, api.Catalog) {
	machine, catalog := fixture()
	objects := []api.Object{}
	for _, existing := range catalog.Objects() {
		switch existing.Kind() {
		case api.NetworkConfig:
			nmstate := m("interfaces", list(m("name", name, "type", "ethernet")), "routes", m("config", list(route)))
			existing = existing.WithSpec(existing.Spec().With("nmstate", nmstate))
		case api.Machine:
			spec := existing.Spec().
				WithPath(list(m("name", name, "macAddress", "02-00-00-00-00-01")), "hardware", "nics").
				WithPath(list(m("name", "primary", "address", "192.0.2.11/24", "interface", name)), "network", "addresses")
			existing = existing.WithSpec(spec)
			machine = existing
		}
		objects = append(objects, existing)
	}
	return machine, api.NewCatalog(objects)
}

func requireOneValueIssue(t *testing.T, issues []api.Issue, field string) {
	t.Helper()
	if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != field || issues[0].Remediation == "" {
		t.Fatalf("issues = %#v, want one api.value at %s", issues, field)
	}
}

// An interface name reaches kernel interfaces and installer directives
// verbatim, so it is a Linux interface name and nothing else.
func TestNativeInterfaceNamesAreLinuxInterfaceNames(t *testing.T) {
	route := m("destination", "0.0.0.0/0", "next-hop-interface", "eth0")
	for name, value := range map[string]string{
		"16 bytes":  strings.Repeat("e", 16),
		"a slash":   "eth0/1",
		"a newline": "eth0\nnetwork --bootproto=dhcp",
	} {
		t.Run(name, func(t *testing.T) {
			machine, catalog := withNative(value, route)
			_, issues := ComposeNetwork(machine, catalog)
			requireOneValueIssue(t, issues, "$.spec.network.configRef.interfaces[0].name")
			config, _ := catalog.Find(api.NetworkConfig, "net")
			requireOneValueIssue(t, ValidateAuthored(config, catalog), "$.spec.nmstate.interfaces[0].name")
		})
	}
	longest := strings.Repeat("e", 15)
	machine, catalog := withNative(longest, m("destination", "0.0.0.0/0", "next-hop-interface", longest))
	if _, issues := ComposeNetwork(machine, catalog); len(issues) != 0 {
		t.Fatalf("a 15-byte interface name was refused: %v", issues)
	}
}

// A route's next hop is the gateway the installation configures, so it is an
// IP literal of its destination's family and the route names a real interface.
func TestARouteNextHopIsAnIPOfItsDestinationFamily(t *testing.T) {
	for name, hop := range map[string]string{
		"not an IP":      "not-an-ip",
		"a newline":      "192.0.2.1\n%post",
		"a zone":         "fe80::1%eth0",
		"another family": "2001:db8::1",
	} {
		t.Run(name, func(t *testing.T) {
			machine, catalog := withNative("eth0", m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", hop))
			_, issues := ComposeNetwork(machine, catalog)
			requireOneValueIssue(t, issues, "$.spec.network.configRef.routes.config[0].next-hop-address")
		})
	}
	machine, catalog := withNative("eth0", m("destination", "0.0.0.0/0", "next-hop-interface", "eth0\n%post"))
	_, issues := ComposeNetwork(machine, catalog)
	requireOneValueIssue(t, issues, "$.spec.network.configRef.routes.config[0].next-hop-interface")
	machine, catalog = withNative("eth0", m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1"))
	if _, issues := ComposeNetwork(machine, catalog); len(issues) != 0 {
		t.Fatalf("an IPv4 next hop of an IPv4 default route was refused: %v", issues)
	}
	machine, catalog = withNative("eth0", m("destination", "::/0", "next-hop-interface", "eth0", "next-hop-address", "fe80::1"))
	if _, issues := ComposeNetwork(machine, catalog); len(issues) != 0 {
		t.Fatalf("an IPv6 link-local next hop of an IPv6 default route was refused: %v", issues)
	}
}
