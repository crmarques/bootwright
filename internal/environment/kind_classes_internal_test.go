package environment

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Selection places every registered kind in exactly one class: a root every
// selection keeps, a cluster an Environment selects, a kind that exists only
// within a cluster (read from the schema registry), or a kind kept only as a
// retained object's dependency. A new kind fails here until it has a class.
func TestSelectionClassifiesEveryKindExactlyOnce(t *testing.T) {
	classes := map[string][]api.Kind{
		"root":       rootKinds(),
		"cluster":    {api.OpenShiftCluster, api.CephCluster},
		"child":      append(api.Children(api.CephCluster), api.Children(api.OpenShiftCluster)...),
		"dependency": {api.Machine, api.InfraProvider, api.ClusterAddon, api.ClusterAddonProfile},
	}
	for _, kind := range api.Kinds() {
		var holders []string
		for class, kinds := range classes {
			if slices.Contains(kinds, kind) {
				holders = append(holders, class)
			}
		}
		if len(holders) != 1 {
			t.Errorf("selection classes %s as %v; give it exactly one class", kind, holders)
		}
	}
	for class, kinds := range classes {
		for _, kind := range kinds {
			if api.KindIndex(kind) < 0 {
				t.Errorf("the %s class names %s, which the catalog does not register", class, kind)
			}
		}
	}
}
