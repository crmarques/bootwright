package main

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/environment/inspection"
)

func containerDetails(catalog api.Catalog, cluster api.Object) inspection.ContainerDetails {
	facts := openshift.Inspect(catalog, cluster)
	details := inspection.ContainerDetails{
		Distribution: facts.Distribution, Release: facts.Release, ReleaseImage: facts.ReleaseImage,
		Platform: facts.Platform, InstallMode: facts.InstallMode, BaseDomain: facts.BaseDomain, IngressDomain: facts.IngressDomain,
		FIPS: facts.FIPS, NetworkType: facts.NetworkType, MachineNetworks: facts.MachineNetworks, ServiceNetworks: facts.ServiceNetworks,
	}
	for _, network := range facts.ClusterNetworks {
		details.ClusterNetworks = append(details.ClusterNetworks, inspection.ContainerNetwork{CIDR: network.CIDR, HostPrefix: network.HostPrefix})
	}
	for _, node := range facts.Nodes {
		details.Nodes = append(details.Nodes, inspection.ContainerNode{Name: node.Name, Role: node.Role, Machine: node.Machine, FQDN: node.FQDN, Address: node.Address})
	}
	for _, endpoint := range facts.Endpoints {
		details.Endpoints = append(details.Endpoints, inspection.ContainerEndpoint{Service: endpoint.Service, URL: endpoint.URL, Address: endpoint.Address, Source: endpoint.Source})
	}
	return details
}
