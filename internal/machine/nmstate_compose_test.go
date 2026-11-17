package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Admission validates the network a Machine composes and realization builds
// it, so both read one merge of the template and its overrides.
func TestAdmissionAndRealizationComposeOneNetwork(t *testing.T) {
	o, c := fixture()
	overrides := m(
		"interfaces", list(m("name", "eth0", "mtu", api.IntegerValue("9000")), m("name", "eth1", "type", "ethernet", "state", "down")),
		"routes", m("config", list(m("next-hop-address", "192.0.2.1"))),
	)
	nics := list(m("name", "eth0", "macAddress", "02-00-00-00-00-01"), m("name", "eth1", "macAddress", "02-00-00-00-00-02"))
	o = o.WithSpec(o.Spec().WithPath(overrides, "network", "overrides").WithPath(nics, "hardware", "nics"))
	objects := []api.Object{o}
	for _, other := range c.Objects() {
		if other.Kind() != api.Machine {
			objects = append(objects, other)
		}
	}
	c = api.NewCatalog(objects)
	composed, issues := ComposeNetwork(o, c)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	realized, err := substrate.NetworkTemplate(c, o)
	if err != nil {
		t.Fatal(err)
	}
	left, right := composed.Get("interfaces").Items(), realized.Get("interfaces").Items()
	if len(left) != 2 || len(left) != len(right) {
		t.Fatalf("composed %v, realized %v", composed.Get("interfaces"), realized.Get("interfaces"))
	}
	for index := range left {
		for _, key := range []string{"name", "type", "state", "mtu"} {
			if !left[index].Get(key).Equal(right[index].Get(key)) {
				t.Fatalf("interface %d %s: composed %v, realized %v", index, key, left[index].Get(key), right[index].Get(key))
			}
		}
	}
	if left[0].Get("mtu").Text() != "9000" {
		t.Fatalf("mtu = %v", left[0].Get("mtu"))
	}
	if !composed.Get("routes", "config").Equal(realized.Get("routes", "config")) {
		t.Fatalf("routes: composed %v, realized %v", composed.Get("routes"), realized.Get("routes"))
	}
	if realized.Get("routes", "config").Items()[0].Get("next-hop-address").Text() != "192.0.2.1" {
		t.Fatalf("routes = %v", realized.Get("routes"))
	}
}
