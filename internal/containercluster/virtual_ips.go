package containercluster

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type VirtualIP struct{ Slot, Address, Bridge string }

func VirtualIPs(catalog api.Catalog, cluster api.Object, controller string) []VirtualIP {
	result := []VirtualIP{}
	if cluster.Spec().Get("nodes").Len() < 2 || cluster.Spec().Get("install", "platform", "type").Text() != "baremetal" {
		return result
	}
	for _, slot := range endpointSlots {
		endpoint := cluster.Spec().Get("install", "endpoints", slot)
		if sourceType(endpoint) != "openshift" {
			continue
		}
		ip := VirtualIP{Slot: slot, Address: endpoint.Get("address").Text()}
		address, err := netip.ParseAddr(ip.Address)
		if err != nil {
			continue
		}
		if address.Is4() && controller != "" {
			for _, provider := range catalog.OfKind(api.InfraProvider) {
				if provider.Spec().Get("libvirt", "machineRef").Text() != controller {
					continue
				}
				for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
					arm := attachment.Get("libvirt")
					prefix, err := netip.ParsePrefix(arm.Get("address").Text())
					if arm.Get("management").Text() == "managed" && err == nil && prefix.Contains(address) {
						ip.Bridge = arm.Get("bridge").Text()
					}
				}
			}
		}
		result = append(result, ip)
	}
	return result
}
