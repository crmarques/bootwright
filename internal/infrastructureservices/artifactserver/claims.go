package artifactserver

import api "github.com/crmarques/bootwright/api/v1alpha1"

func (Capability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, server := range catalog.OfKind(api.ArtifactServer) {
		if server.Spec().Get("management").Text() == "managed" {
			claimed = append(claimed, server.Identity())
		}
	}
	return claimed
}
