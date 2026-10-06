package compilation

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNormalizationOrderIsAPermutationOfTheKindCatalog(t *testing.T) {
	order, catalog := normalizationOrder(), api.Kinds()
	if len(order) != len(catalog) {
		t.Errorf("normalization orders %d kinds, the catalog registers %d", len(order), len(catalog))
	}
	seen := map[api.Kind]bool{}
	for _, kind := range order {
		if seen[kind] {
			t.Errorf("normalization orders %s twice", kind)
		}
		if api.KindIndex(kind) < 0 {
			t.Errorf("normalization orders %s, which the catalog does not register", kind)
		}
		seen[kind] = true
	}
	for _, kind := range catalog {
		if !seen[kind] {
			t.Errorf("normalization never orders %s", kind)
		}
	}
}
