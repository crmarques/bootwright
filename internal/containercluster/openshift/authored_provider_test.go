package openshift

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func authoredNMState(t *testing.T) api.Object {
	t.Helper()
	crd := m("resourceExists", m("apiVersion", "apiextensions.k8s.io/v1", "kind", "CustomResourceDefinition", "name", "widgets.example.test"))
	resource := m("apiVersion", "example.test/v1", "kind", "Widget", "metadata", m("name", "widget", "namespace", "provider-ns"))
	spec := m("provides", api.StringList("nmstate"), "olm", m(
		"namespace", m("name", "provider-ns"), "operatorGroup", m(),
		"subscription", m("package", "provider", "channel", "stable", "startingCSV", "provider.v1", "source", "redhat-operators"),
		"customResources", list(m("gate", list(crd), "resource", resource)),
	))
	normalized, _ := addons.Normalize(obj(api.ClusterAddon, "authnet", spec), api.NewCatalog(nil))
	if issues := addons.Validate(normalized, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	return normalized
}

func TestAnAuthoredNMStateProviderSatisfiesTheNetworkProviderRequirement(t *testing.T) {
	cluster := obj(api.OpenShiftCluster, "cluster", m("networking", m("secondaryNetworks", list(secondaryNetwork()))))
	provider := authoredNMState(t)
	binding := obj(api.ClusterAddonBinding, "binding", m("clusterRef", "cluster", "addonRefs", api.StringList("authnet")))
	catalog := api.NewCatalog([]api.Object{cluster, provider, binding})
	if issues := ValidateNetworkProviders(cluster, catalog, addons.Registry{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	instances, issues := addons.Instances(catalog, addons.Registry{})
	if len(issues) != 0 || len(instances) != 1 || instances[0].Package.Name != "authnet" {
		t.Fatalf("the provider is not selected as an instance: %+v %+v", instances, issues)
	}
	unselected := api.NewCatalog([]api.Object{cluster, provider})
	if issues := ValidateNetworkProviders(cluster, unselected, addons.Registry{}); len(issues) != 1 {
		t.Fatal(issues)
	}
}
