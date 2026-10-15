package installation

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Every spelling of a zero-prefix IPv4 destination is the default route the
// install line carries, and a second default route the line does not carry
// installs too: network convergence applies it after installation.
func TestEveryZeroPrefixSpellingOfTheDefaultRouteIsCarried(t *testing.T) {
	install := api.MapValue(text("name", "enp1s0"), text("type", "ethernet"))
	route := func(destination, hop string, fields ...api.FieldValue) api.Value {
		return api.MapValue(append([]api.FieldValue{text("destination", destination), text("next-hop-address", hop)}, fields...)...)
	}
	for _, destination := range []string{"0.0.0.0/0", "192.0.2.0/0", "10.1.2.3/0"} {
		for name, fields := range map[string][]api.FieldValue{
			"through the install interface": {text("next-hop-interface", "enp1s0")},
			"through no named interface":    nil,
		} {
			t.Run(destination+" "+name, func(t *testing.T) {
				config := withRoutes(networkWith(install), route(destination, "198.51.100.1", fields...))
				if refused := Refusals(labCatalog(config)); len(refused) != 0 {
					t.Fatalf("refusals = %+v, want none", refused)
				}
			})
		}
	}
	for name, routes := range map[string][]api.Value{
		"an IPv6 default route beside the carried one": {route("0.0.0.0/0", "198.51.100.1"), route("::/0", "2001:db8::1")},
		"a second zero-prefix spelling":                {route("0.0.0.0/0", "198.51.100.1"), route("192.0.2.0/0", "198.51.100.1")},
	} {
		t.Run(name, func(t *testing.T) {
			config := withRoutes(networkWith(install), routes...)
			expectNoRefusal(t, labCatalog(config))
		})
	}
}
