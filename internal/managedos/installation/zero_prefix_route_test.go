package installation

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// The refusal and the gateway the Kickstart carries read a route's destination
// one way, so every spelling of a zero-prefix IPv4 destination is the default
// route the install line carries, and a route that is not one is not.
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
	refusal := func(destination string) []lifecycle.Refusal {
		return []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: uncarriedReason,
			Remediation: "remove route " + destination + " from the network of Machine/rhel-01, or install its operating system outside Bootwright"}}
	}
	for name, test := range map[string]struct {
		routes      []api.Value
		destination string
	}{
		"an IPv6 default route beside the carried one": {[]api.Value{route("0.0.0.0/0", "198.51.100.1"), route("::/0", "2001:db8::1")}, "::/0"},
		"a second zero-prefix spelling":                {[]api.Value{route("0.0.0.0/0", "198.51.100.1"), route("192.0.2.0/0", "198.51.100.1")}, "192.0.2.0/0"},
	} {
		t.Run(name, func(t *testing.T) {
			config := withRoutes(networkWith(install), test.routes...)
			if refused := Refusals(labCatalog(config)); !slices.Equal(refused, refusal(test.destination)) {
				t.Fatalf("refusals = %+v, want %+v", refused, refusal(test.destination))
			}
		})
	}
}
