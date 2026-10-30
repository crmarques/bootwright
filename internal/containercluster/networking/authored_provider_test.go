package networking

import (
	"context"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func authoredProvider(t *testing.T, name string) api.Object {
	t.Helper()
	crd := mapping(field("resourceExists", mapping(field("apiVersion", text("apiextensions.k8s.io/v1")), field("kind", text("CustomResourceDefinition")), field("name", text("widgets.example.test")))))
	resource := mapping(field("apiVersion", text("example.test/v1")), field("kind", text("Widget")), field("metadata", mapping(field("name", text("widget")), field("namespace", text("provider-ns")))))
	spec := mapping(
		field("provides", api.StringList("nmstate")),
		field("olm", mapping(
			field("namespace", mapping(field("name", text("provider-ns")))),
			field("operatorGroup", mapping()),
			field("subscription", mapping(field("package", text("provider")), field("channel", text("stable")), field("startingCSV", text("provider.v1")), field("source", text("redhat-operators")))),
			field("customResources", api.ListValue(mapping(field("gate", api.ListValue(crd)), field("resource", resource)))),
		)),
	)
	normalized, issues := addons.Normalize(api.NewObject(api.ClusterAddon, name, mapping(), spec), api.NewCatalog(nil))
	if len(issues) != 0 || len(addons.Validate(normalized, api.NewCatalog(nil))) != 0 {
		t.Fatal(issues, addons.Validate(normalized, api.NewCatalog(nil)))
	}
	return normalized
}

func TestAnAuthoredNMStateProviderSatisfiesTheNetworkBlockLikeABuiltInPackage(t *testing.T) {
	builtin := addons.Package{Name: "netw", Summary: "fixture package", Provides: []string{"nmstate"}, Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}
	registry, err := addons.NewRegistry([]addons.Package{builtin})
	if err != nil {
		t.Fatal(err)
	}
	environment := api.NewObject(api.Environment, "env", mapping(), mapping(field("controller", mapping(field("machineRef", text("controller"))))))
	binding := func(fields ...api.FieldValue) api.Object {
		return api.NewObject(api.ClusterAddonBinding, "binding", mapping(), mapping(append([]api.FieldValue{field("clusterRef", text("cluster"))}, fields...)...))
	}
	requirements := func(objects ...api.Object) []reconciliation.ObjectRef {
		t.Helper()
		catalog := api.NewCatalog(append([]api.Object{environment, clusterFixture()}, objects...))
		plan, err := New(networkRunner{}, registry).Plan(context.Background(), lifecycle.PlanInput{State: compilation.NewState(catalog, catalog, nil), Verb: reconciliation.Apply})
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("%+v %v", plan, err)
		}
		return plan.Definitions[0].Requires
	}
	want := []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: "cluster"}, {Kind: "ClusterAddonBinding", Object: "cluster.netw"}}
	if got := requirements(binding(field("packages", mapping(field("netw", mapping()))))); !slices.Equal(got, want) {
		t.Fatalf("built-in: %v", got)
	}
	want[1].Object = "cluster.authnet"
	if got := requirements(binding(field("addonRefs", api.StringList("authnet"))), authoredProvider(t, "authnet")); !slices.Equal(got, want) {
		t.Fatalf("authored: %v", got)
	}
}
