package managedservice

import api "github.com/crmarques/bootwright/api/v1alpha1"

func (c Capability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, service := range ManagedObjects(catalog, c.definition) {
		claimed = append(claimed, service.Identity())
	}
	return claimed
}
