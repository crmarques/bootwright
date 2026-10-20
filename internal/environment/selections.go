package environment

import api "github.com/crmarques/bootwright/api/v1alpha1"

func ContainerClusterDomain(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	return environments[0].Spec().Get("domains", "containerClusters").Text()
}
