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
	Slot    string
	Subtree bool
}

const ConsolePrefix = "console-openshift-console."

func WildcardLabel(cluster string) string {
	label := "bootwright-" + cluster
	if len(label) > 63 {
		label = label[:63]
	}
	return strings.TrimRight(label, "-")
}

func ResolutionNames(catalog api.Catalog, cluster api.Object) []Endpoint {
	var names []Endpoint
	for _, endpoint := range Endpoints(catalog, cluster) {
		if !endpoint.Subtree {
			names = append(names, endpoint)
			continue
		}
		names = append(names,
			Endpoint{Name: ConsolePrefix + endpoint.Name, Address: endpoint.Address, Slot: "ingress"},
			Endpoint{Name: WildcardLabel(cluster.Name()) + "." + endpoint.Name, Address: endpoint.Address, Slot: "ingress-wildcard"})
	}
	return names
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
		endpoints = append(endpoints, Endpoint{Name: name + suffix, Address: address, Slot: slot, Subtree: slot == "ingress"})
	}
	slices.SortFunc(endpoints, func(a, b Endpoint) int { return strings.Compare(a.Name, b.Name) })
	return endpoints
}
