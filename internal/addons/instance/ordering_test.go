package instance_test

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestAuthoredAfterRefsBecomeInstanceRequirements(t *testing.T) {
	registry := packages.Builtin()
	subscription := api.MapValue(api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("operator.v1")})})
	binding := func(name, member string, after ...string) api.Object {
		fields := []api.FieldValue{{Name: "clusterRef", Value: api.StringValue("cluster")}, {Name: "packages", Value: api.MapValue(api.FieldValue{Name: member, Value: subscription})}}
		if len(after) > 0 {
			fields = append(fields, api.FieldValue{Name: "afterRefs", Value: api.StringList(after...)})
		}
		object, _ := addons.NormalizePackages(api.NewObject(api.ClusterAddonBinding, name, api.MapValue(), api.MapValue(fields...)), registry)
		return object
	}
	for _, authored := range []bool{false, true} {
		balancer := binding("balancer", "metallb")
		if authored {
			balancer = binding("balancer", "metallb", "network")
		}
		selected, issues := addons.Instances(api.NewCatalog([]api.Object{balancer, binding("network", "nmstate")}), registry)
		if len(issues) != 0 || len(selected) != 2 {
			t.Fatal(selected, issues)
		}
		at := slices.IndexFunc(selected, func(s addons.SelectedInstance) bool { return s.Package.Name == "metallb" })
		_, requires, err := instance.Render(selected[at], addons.ClusterFacts{Release: "4.20"}, "controller")
		if err != nil {
			t.Fatal(err)
		}
		edge := slices.Contains(requires, reconciliation.ObjectRef{Kind: instance.Kind, Object: instance.Object("cluster", "nmstate")})
		if edge != authored || authored && at != 1 {
			t.Fatalf("authored %v: metallb at %d requires %v", authored, at, requires)
		}
	}
}
