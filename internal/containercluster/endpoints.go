package containercluster

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

type Endpoint struct {
	Name    string
	Address string
	Subtree bool
}

func Endpoints(catalog api.Catalog, cluster api.Object) []Endpoint {
	zone := environment.ContainerClusterDomain(catalog)
	if zone == "" {
		return nil
	}
	suffix := "." + cluster.Name() + "." + zone
	var endpoints []Endpoint
	for _, slot := range []string{"api", "api-int", "ingress"} {
		address := cluster.Spec().Get("install", "endpoints", slot, "address").Text()
		if address == "" {
			continue
		}
		name := slot
		if slot == "ingress" {
			name = "apps"
		}
		endpoints = append(endpoints, Endpoint{Name: name + suffix, Address: address, Subtree: slot == "ingress"})
	}
	slices.SortFunc(endpoints, func(a, b Endpoint) int { return strings.Compare(a.Name, b.Name) })
	return endpoints
}
