package openshift

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

type VirtualIP struct{ Slot, Address, Bridge string }

func VirtualIPs(catalog api.Catalog, cluster api.Object, controller string) []VirtualIP {
	result := []VirtualIP{}
	if cluster.Spec().Get("nodes").Len() < 2 || Platform(cluster.Spec()) != "baremetal" {
		return result
	}
	for _, slot := range endpointSlots {
		endpoint := cluster.Spec().Get("install", "endpoints", slot)
		if sourceType(endpoint) != "installer" {
			continue
		}
		ip := VirtualIP{Slot: slot, Address: endpoint.Get("address").Text()}
		address, err := netip.ParseAddr(ip.Address)
		if err != nil {
			continue
		}
		if address.Is4() && controller != "" {
			for _, provider := range catalog.OfKind(api.InfraProvider) {
				if substrate.HostMachine(provider) != controller {
					continue
				}
				for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
					network, managed := substrate.ManagedAttachment(attachment)
					prefix, err := netip.ParsePrefix(network.Prefix)
					if managed && err == nil && prefix.Contains(address) {
						ip.Bridge = network.Bridge
					}
				}
			}
		}
		result = append(result, ip)
	}
	return result
}
