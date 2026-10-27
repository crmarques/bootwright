package main

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

// Every registered kind either bears an effect, by exactly one domain's
// predicate (a managed infrastructure service by its own), or is listed here
// as bearing none. A new kind fails until its domain decides.
func TestEveryKindIsEffectBearingByExactlyOneDomainOrListedAsBearingNone(t *testing.T) {
	none := []api.Kind{api.Environment, api.Entitlement, api.MachineImage, api.MachineInstallProfile, api.NetworkConfig,
		api.ClusterAddon, api.ClusterAddonProfile, api.Secret}
	bearing := api.MapValue(
		api.FieldValue{Name: "management", Value: api.StringValue("managed")},
		api.FieldValue{Name: "enabled", Value: api.BoolValue(true)},
		api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(false)})},
	)
	for _, kind := range api.Kinds() {
		object := api.NewObject(kind, "probe", api.Value{}, bearing)
		holders := 0
		if _, bears := infrastructureservices.EffectBearing(object); bears {
			holders++
		}
		for _, predicate := range domainEffects() {
			if _, bears := predicate(object); bears {
				holders++
			}
		}
		_, composed := effectBearing(object)
		switch {
		case slices.Contains(none, kind) && (holders != 0 || composed):
			t.Errorf("%s is listed as bearing no effect, but %d domains claim it", kind, holders)
		case !slices.Contains(none, kind) && (holders != 1 || !composed):
			t.Errorf("%s bears an effect by %d domains; exactly one owns it", kind, holders)
		}
	}
}
