package installation

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func overridden(overrides api.Value) api.Object {
	return guest(field("network", guest().Spec().Get("network").With("overrides", overrides)))
}

func overridingRoutes(routes ...api.Value) api.Value {
	return api.MapValue(field("routes", api.MapValue(field("config", api.ListValue(routes...)))))
}

// The install line carries the gateway of the network the Machine composes, so
// an override that moves it is installed rather than refused.
func TestAnOverriddenGatewayIsTheOneTheLineCarries(t *testing.T) {
	catalog := labCatalog(overridden(overridingRoutes(api.MapValue(
		text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.254"),
	))))
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, _ := onlyRequest(t, catalog)
	if !strings.Contains(request.Kickstart, "--gateway=198.51.100.254") || strings.Contains(request.Kickstart, "--gateway=198.51.100.1") {
		t.Fatalf("kickstart = %s", request.Kickstart)
	}
}

// A default route the override removes is not installed.
func TestAnAbsentDefaultRouteIsNotInstalled(t *testing.T) {
	catalog := labCatalog(overridden(overridingRoutes(api.MapValue(text("state", "absent")))))
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, _ := onlyRequest(t, catalog)
	if strings.Contains(request.Kickstart, "--gateway=") {
		t.Fatalf("kickstart = %s", request.Kickstart)
	}
}

// An absent default route ahead of the install interface's route leaves that
// route the one the line carries.
func TestTheGatewaySkipsAnAbsentRouteForTheInstallInterfacesRoute(t *testing.T) {
	config := withRoutes(networkConfig(),
		api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), text("next-hop-interface", "enp1s0"), text("state", "absent")),
		api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.2"), text("next-hop-interface", "enp1s0")),
	)
	catalog := labCatalog(config)
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, _ := onlyRequest(t, catalog)
	if !strings.Contains(request.Kickstart, "--gateway=198.51.100.2") {
		t.Fatalf("kickstart = %s", request.Kickstart)
	}
}

// The domain attaches the interfaces the composed network keeps available.
func TestAnOverrideAddedInterfaceIsRealized(t *testing.T) {
	ethernet := func(name string) api.Value { return api.MapValue(text("name", name), text("type", "ethernet")) }
	for name, test := range map[string]struct {
		config    api.Object
		overrides api.Value
		want      []string
	}{
		"an added interface": {networkConfig(), api.MapValue(field("interfaces", api.ListValue(ethernet("enp9s0")))), []string{"enp1s0", "enp9s0"}},
		"an absent interface": {networkWith(ethernet("enp1s0"), ethernet("enp2s0")),
			api.MapValue(field("interfaces", api.ListValue(api.MapValue(text("name", "enp2s0"), text("state", "absent"))))), []string{"enp1s0"}},
	} {
		t.Run(name, func(t *testing.T) {
			machine := overridden(test.overrides)
			target, err := substrate.TargetFor(labCatalog(test.config, machine), machine, testContext, "controller")
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, iface := range target.Interfaces {
				names = append(names, iface.Name)
			}
			if !slices.Equal(names, test.want) {
				t.Fatalf("interfaces = %v, want %v", names, test.want)
			}
		})
	}
}
