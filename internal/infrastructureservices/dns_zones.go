package infrastructureservices

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func LocalDNSZones(catalog api.Catalog) []string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil
	}
	base := environments[0].Spec().Get("domains", "containerClusters").Text()
	if base == "" {
		return nil
	}
	var zones []string
	for _, cluster := range catalog.OfCategory(api.ContainerClusterCategory) {
		zones = append(zones, cluster.Name()+"."+base)
	}
	slices.Sort(zones)
	return slices.Compact(zones)
}

func validateLocalDNSZones(value api.Value, catalog api.Catalog) []api.Issue {
	var issues []api.Issue
	for i, forward := range value.Get("forwardZones").Items() {
		domain := forward.Get("domain").Text()
		for _, local := range LocalDNSZones(catalog) {
			if domain == local || strings.HasSuffix(domain, "."+local) {
				issues = append(issues, issue(fmt.Sprintf("$.spec.forwardZones[%d].domain", i),
					"DNS forwarding domain "+domain+" is inside local cluster zone "+local,
					"remove this forwarding zone; the managed resolver answers its cluster zones locally"))
			}
		}
	}
	return issues
}
