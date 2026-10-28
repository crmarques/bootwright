package instance

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func (c Capability) Claimed(catalog api.Catalog) []string {
	selected, _ := addons.Instances(catalog, c.registry)
	var claimed []string
	for _, instance := range selected {
		claimed = append(claimed, string(api.ClusterAddonBinding)+"/"+instance.Instance.Binding)
	}
	slices.Sort(claimed)
	return slices.Compact(claimed)
}
