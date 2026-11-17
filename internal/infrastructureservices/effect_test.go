package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEffectBearingNamesOnlyManagedServices(t *testing.T) {
	for name, test := range map[string]struct {
		object  api.Object
		bearing bool
	}{
		"a managed proxy":     {obj(api.Proxy, "p", m("management", "managed")), true},
		"an external proxy":   {obj(api.Proxy, "p", m("management", "external")), false},
		"a managed machine":   {obj(api.Machine, "m", m("management", "managed")), false},
		"a service unmanaged": {obj(api.DNSServer, "d", m()), false},
	} {
		reason, bearing := EffectBearing(test.object)
		if bearing != test.bearing || bearing != (reason != "") {
			t.Errorf("%s: EffectBearing = %q, %t", name, reason, bearing)
		}
	}
	reason, _ := EffectBearing(obj(api.NTPServer, "t", m("management", "managed")))
	if want := "no capability of this executable manages the NTPServer kind"; reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
}

func TestUnclaimedRemedyIsOfferedOnlyForALoadBalancer(t *testing.T) {
	balancer := obj(api.LoadBalancer, "front", m())
	want := "declare spec.management: external on " + balancer.Identity() + " to use a LoadBalancer the operator runs, or remove it from the selected Environment"
	if got := UnclaimedRemedy(balancer); got != want {
		t.Errorf("remedy = %q, want %q", got, want)
	}
	if got := UnclaimedRemedy(obj(api.Proxy, "p", m())); got != "" {
		t.Errorf("a proxy got remedy %q", got)
	}
}

func TestHostsManagedServiceReadsPlacementOfManagedServicesOnly(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{
		obj(api.Proxy, "managed", m("management", "managed", "machineRef", "edge")),
		obj(api.DNSServer, "external", m("management", "external", "machineRef", "other")),
		obj(api.Machine, "machine-kind", m("management", "managed", "machineRef", "third")),
	})
	if Placement(obj(api.Proxy, "x", m("machineRef", "edge"))) != "edge" || Placement(obj(api.Proxy, "x", m())) != "" {
		t.Error("Placement does not read spec.machineRef")
	}
	for machine, want := range map[string]bool{"edge": true, "other": false, "third": false, "absent": false} {
		if got := HostsManagedService(catalog, machine); got != want {
			t.Errorf("HostsManagedService(%q) = %t, want %t", machine, got, want)
		}
	}
}
