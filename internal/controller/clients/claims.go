package clients

import api "github.com/crmarques/bootwright/api/v1alpha1"

func (Capability) Claimed(catalog api.Catalog) []string {
	return identities(catalog.OfKind(api.Environment))
}

func identities(objects []api.Object) []string {
	found := make([]string, 0, len(objects))
	for _, object := range objects {
		found = append(found, object.Identity())
	}
	return found
}
