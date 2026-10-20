package containercluster

import (
	"fmt"
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func endpointCollisions(cluster api.Object, catalog api.Catalog, slot string, endpoint api.Value, address netip.Addr) []api.Issue {
	var issues []api.Issue
	field := "$.spec.install.endpoints." + slot + ".address"
	source := sourceType(endpoint)
	if source == "node" || source == "loadBalancer" {
		field = "$.spec.install.endpoints." + slot + ".source"
	}
	claim := func(holder api.Object, path, raw string) {
		other, err := netip.ParseAddr(raw)
		if prefix, e := netip.ParsePrefix(raw); e == nil {
			other, err = prefix.Addr(), nil
		}
		if err != nil || other.IsUnspecified() || other.Unmap() != address.Unmap() {
			return
		}
		issues = add(issues, invariantRemediation(field, fmt.Sprintf("endpoint address %s is already held by %s at %s", address, holder.Identity(), path), "choose an endpoint address distinct from the other declared holder"))
	}
	for _, holder := range catalog.Objects() {
		spec := holder.Spec()
		switch holder.Kind() {
		case api.ContainerCluster:
			if holder.Name() == cluster.Name() {
				continue
			}
			for _, otherSlot := range endpointSlots {
				claim(holder, "spec.install.endpoints."+otherSlot+".address", spec.Get("install", "endpoints", otherSlot, "address").Text())
			}
		case api.Machine:
			if source == "node" && cluster.Spec().Get("nodes").Len() == 1 && cluster.Spec().Get("nodes").Items()[0].Get("machineRef").Text() == holder.Name() {
				continue
			}
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
				if source == "loadBalancer" && endpoint.Get("source", "loadBalancerRef").Text() == holder.Name() && (entry.Get("name").Text() == endpoint.Get("source", "bindAddressRef").Text() || spec.Get("bindAddresses").Len() == 1 && endpoint.Get("source", "bindAddressRef").Text() == "") {
					continue
				}
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
					for _, hostAddress := range host.Spec().Get("network", "addresses").Items() {
						if hostAddress.Get("name").Text() == entry.Get("addressRef").Text() {
							claim(holder, fmt.Sprintf("spec.endpoints[%d].addressRef", i), hostAddress.Get("address").Text())
						}
					}
				}
			}
		}
	}
	return issues
}
