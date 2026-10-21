package containercluster

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type addressClaim struct {
	Object  api.Object
	Path    string
	Address netip.Addr
}

func addressClaims(catalog api.Catalog) []addressClaim {
	var claims []addressClaim
	claim := func(holder api.Object, path, raw string) {
		address, err := netip.ParseAddr(raw)
		if prefix, e := netip.ParsePrefix(raw); e == nil {
			address, err = prefix.Addr(), nil
		}
		if err == nil && !address.IsUnspecified() {
			claims = append(claims, addressClaim{holder, path, address.Unmap()})
		}
	}
	for _, holder := range catalog.Objects() {
		spec := holder.Spec()
		switch holder.Kind() {
		case api.ContainerCluster:
			for _, slot := range endpointSlots {
				claim(holder, "spec.install.endpoints."+slot+".address", spec.Get("install", "endpoints", slot, "address").Text())
			}
		case api.Machine:
			for i, entry := range spec.Get("network", "addresses").Items() {
				claim(holder, fmt.Sprintf("spec.network.addresses[%d].address", i), entry.Get("address").Text())
			}
		case api.InfraProvider:
			for i, entry := range spec.Get("networkAttachments").Items() {
				if entry.Get("libvirt", "management").Text() == "managed" {
					claim(holder, fmt.Sprintf("spec.networkAttachments[%d].libvirt.address", i), entry.Get("libvirt", "address").Text())
				}
			}
			claim(holder, "spec.libvirt.bmcEmulationDefaults.bindAddress", spec.Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text())
		case api.LoadBalancer:
			for i, entry := range spec.Get("bindAddresses").Items() {
				claim(holder, fmt.Sprintf("spec.bindAddresses[%d].address", i), entry.Get("address").Text())
			}
		case api.DNSServer, api.NTPServer, api.Proxy, api.ArtifactServer, api.Registry:
			if spec.Get("management").Text() != "managed" {
				continue
			}
			claim(holder, "spec.bindAddress", spec.Get("bindAddress").Text())
			host, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
			for i, entry := range spec.Get("endpoints").Items() {
				claim(holder, fmt.Sprintf("spec.endpoints[%d].address", i), entry.Get("address").Text())
				if found {
					for _, address := range host.Spec().Get("network", "addresses").Items() {
						if address.Get("name").Text() == entry.Get("addressRef").Text() {
							claim(holder, fmt.Sprintf("spec.endpoints[%d].addressRef", i), address.Get("address").Text())
						}
					}
				}
			}
		}
	}
	slices.SortFunc(claims, func(a, b addressClaim) int {
		if n := strings.Compare(a.Object.Identity(), b.Object.Identity()); n != 0 {
			return n
		}
		return strings.Compare(a.Path, b.Path)
	})
	return claims
}

func endpointCollisions(cluster api.Object, catalog api.Catalog, slot string, endpoint api.Value, address netip.Addr) []api.Issue {
	var issues []api.Issue
	field := "$.spec.install.endpoints." + slot + ".address"
	source := sourceType(endpoint)
	if source == "node" || source == "loadBalancer" {
		field = "$.spec.install.endpoints." + slot + ".source"
	}
	for _, claim := range addressClaims(catalog) {
		if claim.Address != address.Unmap() || endpointOwnsClaim(cluster, catalog, endpoint, claim) {
			continue
		}
		issues = add(issues, invariantRemediation(field, fmt.Sprintf("endpoint address %s is already held by %s at %s", address, claim.Object.Identity(), claim.Path), "choose an endpoint address distinct from the other declared holder"))
	}
	return issues
}

func endpointOwnsClaim(cluster api.Object, catalog api.Catalog, endpoint api.Value, claim addressClaim) bool {
	holder := claim.Object
	if holder.Kind() == api.ContainerCluster {
		return holder.Name() == cluster.Name()
	}
	if holder.Kind() == api.Machine {
		return sourceType(endpoint) == "node" && cluster.Spec().Get("nodes").Len() == 1 && cluster.Spec().Get("nodes").Items()[0].Get("machineRef").Text() == holder.Name()
	}
	if holder.Kind() != api.LoadBalancer || sourceType(endpoint) != "loadBalancer" || endpoint.Get("source", "loadBalancerRef").Text() != holder.Name() {
		return false
	}
	for i, entry := range holder.Spec().Get("bindAddresses").Items() {
		if claim.Path == fmt.Sprintf("spec.bindAddresses[%d].address", i) {
			return entry.Get("name").Text() == endpoint.Get("source", "bindAddressRef").Text() || holder.Spec().Get("bindAddresses").Len() == 1 && endpoint.Get("source", "bindAddressRef").Text() == ""
		}
	}
	return false
}
