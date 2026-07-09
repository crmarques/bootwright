package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cataloguedExtension() v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "catalogued"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "cat-ns", Create: true},
				CatalogSource: &v1alpha1.ClusterAddonOLMCatalogSource{
					Name:         "partner-catalog",
					Image:        "icr.io/cpopen/partner-catalog:v1",
					DisplayName:  "Partner Catalog",
					Publisher:    "Partner",
					PollInterval: "45m",
				},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name: "cat-op", Package: "cat-op", Channel: "stable",
					Source: "partner-catalog", SourceNamespace: "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
			},
		},
	}
}

func TestCatalogResourcesRenderShippedCatalog(t *testing.T) {
	resources, err := CatalogResources(cataloguedExtension())
	if err != nil {
		t.Fatalf("CatalogResources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(resources))
	}
	catalog := resources[0]
	if catalog.Kind != "CatalogSource" || catalog.Name != "partner-catalog" || catalog.Namespace != "openshift-marketplace" {
		t.Fatalf("unexpected catalog identity: %s/%s/%s", catalog.Kind, catalog.Namespace, catalog.Name)
	}
	spec, ok := catalog.Object["spec"].(map[string]any)
	if !ok {
		t.Fatalf("catalog spec missing: %v", catalog.Object)
	}
	want := map[string]any{
		"sourceType":  "grpc",
		"image":       "icr.io/cpopen/partner-catalog:v1",
		"displayName": "Partner Catalog",
		"publisher":   "Partner",
		"updateStrategy": map[string]any{
			"registryPoll": map[string]any{"interval": "45m"},
		},
	}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("catalog spec = %v, want %v", spec, want)
	}
}

func TestCatalogResourcesEmptyWithoutShippedCatalog(t *testing.T) {
	extension := cataloguedExtension()
	extension.Spec.OLM.CatalogSource = nil
	resources, err := CatalogResources(extension)
	if err != nil {
		t.Fatalf("CatalogResources: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("resources = %d, want 0", len(resources))
	}
}

func TestOLMResourcesPrependShippedCatalog(t *testing.T) {
	resources, err := OLMResources(cataloguedExtension())
	if err != nil {
		t.Fatalf("OLMResources: %v", err)
	}
	var kinds []string
	for _, resource := range resources {
		kinds = append(kinds, resource.Kind)
	}
	want := []string{"CatalogSource", "Namespace", "Subscription"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}
