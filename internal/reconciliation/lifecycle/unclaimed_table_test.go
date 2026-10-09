package lifecycle

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// unclaimedCase is a selected graph one row of the unclaimed-object table
// refuses, the kinds this build claims, and the object and kind its row names.
type unclaimedCase struct {
	catalog api.Catalog
	claimed []string
	object  api.Object
}

// TestUnclaimedObjectTableMatchesTheEngine holds the engine's refusal of what
// no capability claims to the state owner's table: every row refuses its graph
// with exactly its reason and remedy, an input of the same kind is never
// refused, and a row added or removed on either side fails here.
func TestUnclaimedObjectTableMatchesTheEngine(t *testing.T) {
	managed := func(kind api.Kind, name, management string) api.Object {
		return api.NewObject(kind, name, api.Value{}, api.MapValue(api.FieldValue{Name: "management", Value: api.StringValue(management)}))
	}
	machine := func(name string, provided bool) api.Object {
		return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
			api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(provided)})}))
	}
	playbook := func(name string, enabled bool) api.Object {
		return api.NewObject(api.CustomPlaybook, name, api.Value{}, api.MapValue(api.FieldValue{Name: "enabled", Value: api.BoolValue(enabled)}))
	}
	storage := api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue())
	proxy := managed(api.Proxy, "lab-proxy", "managed")
	bare := machine("bare", false)
	tune := playbook("tune", true)
	cases := map[string]unclaimedCase{
		"An unclaimed effect-bearing kind": {api.NewCatalog([]api.Object{storage}), []string{"Machine"}, storage},
		"An unclaimed managed service": {api.NewCatalog([]api.Object{proxy, managed(api.Proxy, "upstream", "external")}),
			nil, proxy},
		"A Machine to install with no capability": {api.NewCatalog([]api.Object{bare, machine("ready", true)}), nil, bare},
		"An enabled CustomPlaybook":               {api.NewCatalog([]api.Object{tune, playbook("idle", false)}), nil, tune},
	}
	data, err := os.ReadFile(transitionSpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range specTable(t, strings.Split(string(data), "\n"), "<!-- unclaimed objects -->", "Refusal", "Path", "Reason", "Remedy") {
		name := row[0]
		test, found := cases[name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", name)
			continue
		}
		delete(cases, name)
		t.Run(name, func(t *testing.T) {
			expand := func(text string) string {
				return strings.NewReplacer("<kind>", string(test.object.Kind()), "<object>", test.object.Identity()).Replace(text)
			}
			want := []Refusal{RefusalOf(test.object, expand(specCode(t, row[2])), expand(specCode(t, row[3])))}
			if got := Unrealizable(test.catalog, test.claimed); !reflect.DeepEqual(got, want) {
				t.Fatalf("the engine refuses %+v, the table %+v", got, want)
			}
			if got := Unrealizable(test.catalog, []string{string(test.object.Kind())}); len(got) != 0 {
				t.Fatalf("a claimed kind is still refused: %+v", got)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
}
