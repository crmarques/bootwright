package instance_test

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/addons/packages"
)

func gitopsSelection(t *testing.T) addons.SelectedInstance {
	t.Helper()
	registry := packages.Builtin()
	pkg, _ := registry.Find("gitops")
	text := func(value string) api.Value { return api.StringValue(value) }
	subscription := api.MapValue(api.FieldValue{Name: "channel", Value: text("latest")}, api.FieldValue{Name: "startingCSV", Value: text("gitops.v1")})
	bootstrap := api.MapValue(api.FieldValue{Name: "repoURL", Value: text("https://example.test/repo.git")}, api.FieldValue{Name: "targetRevision", Value: text("main")}, api.FieldValue{Name: "path", Value: text(".")}, api.FieldValue{Name: "destinationNamespace", Value: text("application")}, api.FieldValue{Name: "credentialsRef", Value: text("repository-login")})
	config := api.MapValue(api.FieldValue{Name: "subscription", Value: subscription}, api.FieldValue{Name: "bootstrap", Value: bootstrap})
	binding := api.NewObject(api.ClusterAddonBinding, "hub", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: text("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "gitops", Value: config})}))
	binding, _ = addons.NormalizePackages(binding, registry)
	return addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "hub", Config: binding.Spec().Get("packages", "gitops")}}
}

func TestRenamedGitOpsDescriptorStillRendersItsPlacement(t *testing.T) {
	selected := gitopsSelection(t)
	selected.Package.Name = "fixture"
	request, _, err := instance.Render(selected, addons.ClusterFacts{Release: "4.20"}, "controller")
	if err != nil {
		t.Fatalf("a renamed descriptor with the same placement namespaces must render: %v", err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	thawed, err := instance.DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(thawed.PlacementNamespaces, []string{"openshift-gitops"}) {
		t.Fatalf("%v", thawed.PlacementNamespaces)
	}
	if _, err := thawed.Objects(); err != nil {
		t.Fatal(err)
	}
}

func TestGitOpsPlacementIntoKubeSystemRefuses(t *testing.T) {
	selected := gitopsSelection(t)
	render := selected.Package.Render
	selected.Package.Render = func(i addons.Instance) (addons.Rendered, error) {
		rendered, err := render(i)
		for _, phases := range [][]co.Phase{rendered.Before, rendered.After} {
			for _, phase := range phases {
				for k := range phase.Secrets {
					phase.Secrets[k].Namespace = "kube-system"
				}
			}
		}
		return rendered, err
	}
	_, _, err := instance.Render(selected, addons.ClusterFacts{Release: "4.20"}, "controller")
	if err == nil || !strings.Contains(err.Error(), "invalid source or namespace") {
		t.Fatalf("a gitops placement into kube-system rendered: %v", err)
	}
}
