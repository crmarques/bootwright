package openshift

import (
	"cmp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
)

type Inspection struct {
	Distribution, Release, ReleaseImage, Platform, InstallMode, BaseDomain, IngressDomain string
	FIPS                                                                                  bool
	NetworkType                                                                           string
	MachineNetworks, ServiceNetworks                                                      []string
	ClusterNetworks                                                                       []InspectionNetwork
	Nodes                                                                                 []InspectionNode
	Endpoints                                                                             []InspectionEndpoint
}

type InspectionNetwork struct {
	CIDR       string
	HostPrefix int
}

type InspectionNode struct {
	Name, Role, Machine, FQDN, Address string
}

type InspectionEndpoint struct {
	Service, URL, Address, Source string
}

func Inspect(catalog api.Catalog, cluster api.Object) Inspection {
	spec := cluster.Spec()
	result := Inspection{
		Distribution: spec.Get("distribution", "type").Text(), Release: spec.Get("distribution", "release", "version").Text(),
		ReleaseImage: spec.Get("distribution", "release", "image").Text(), Platform: Platform(spec),
		InstallMode: spec.Get("install", "mode").Text(), BaseDomain: environment.ContainerClusterDomain(catalog),
		FIPS: spec.Get("security", "fips", "enabled").Bool(), NetworkType: spec.Get("networking", "networkType").Text(),
		ServiceNetworks: spec.Get("networking", "serviceNetwork").Strings(),
	}
	for _, prefix := range MachineNetworks(cluster, catalog) {
		result.MachineNetworks = append(result.MachineNetworks, prefix.String())
	}
	for _, network := range spec.Get("networking", "clusterNetwork").Items() {
		prefix, _ := network.Get("hostPrefix").Int64()
		result.ClusterNetworks = append(result.ClusterNetworks, InspectionNetwork{CIDR: network.Get("cidr").Text(), HostPrefix: int(prefix)})
	}
	for _, node := range spec.Get("nodes").Items() {
		row := InspectionNode{Name: node.Get("name").Text(), Role: node.Get("role").Text(), Machine: node.Get("machineRef").Text(), FQDN: node.Get("fqdn").Text()}
		if host, found := catalog.Find(api.Machine, row.Machine); found {
			if address, issues := machine.InstallAddress(host, catalog); len(issues) == 0 {
				row.Address = address.Get("address").Text()
			}
		}
		result.Nodes = append(result.Nodes, row)
	}
	slices.SortFunc(result.Nodes, func(a, b InspectionNode) int { return cmp.Compare(a.Name, b.Name) })
	for _, endpoint := range Endpoints(catalog, cluster) {
		service, host := endpoint.Slot, endpoint.Name
		if endpoint.Subtree {
			result.IngressDomain = endpoint.Name
			service, host = "console", ConsolePrefix+endpoint.Name
		} else {
			host += ":6443"
		}
		result.Endpoints = append(result.Endpoints, InspectionEndpoint{
			Service: service, URL: "https://" + host, Address: endpoint.Address,
			Source: sourceType(spec.Get("install", "endpoints", endpoint.Slot)),
		})
	}
	slices.SortFunc(result.Endpoints, func(a, b InspectionEndpoint) int { return cmp.Compare(a.Service, b.Service) })
	return result
}
