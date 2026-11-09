package libvirt

import (
	"cmp"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/network"
	"github.com/crmarques/bootwright/internal/substrate"
)

const MaxRefusedPrefixes = 64

func refusedPrefixes(catalog api.Catalog, provider api.Object) []string {
	host := substrate.HostMachine(provider)
	var found []netip.Prefix
	for _, managed := range substrate.ManagedNetworksOf(catalog, provider) {
		for _, name := range managed.Spec().Get(substrate.ArmLibvirt, "taggedNetworkRefs").Strings() {
			tagged, ok := catalog.Find(api.Network, name)
			if !ok {
				continue
			}
			for _, family := range []string{"ipv4", "ipv6"} {
				if prefix, declared := network.Prefix(tagged, family); declared && !holdsHostAddress(catalog, tagged, prefix, host) {
					found = append(found, prefix)
				}
			}
		}
	}
	slices.SortFunc(found, func(x, y netip.Prefix) int {
		return cmp.Or(x.Addr().Compare(y.Addr()), cmp.Compare(x.Bits(), y.Bits()))
	})
	found = slices.Compact(found)
	var refused []string
	for _, prefix := range found {
		refused = append(refused, prefix.String())
	}
	return refused
}

func holdsHostAddress(catalog api.Catalog, tagged api.Object, prefix netip.Prefix, host string) bool {
	if substrate.IsManagedNetwork(tagged) {
		owner, found := catalog.Find(api.InfraProvider, substrate.ManagedNetworkProvider(tagged))
		if found && substrate.HostMachine(owner) == host {
			return true
		}
	}
	machine, found := catalog.Find(api.Machine, host)
	if !found {
		return false
	}
	return slices.ContainsFunc(machineref.IPAddresses(machine), func(value string) bool {
		address, err := netip.ParseAddr(value)
		return err == nil && prefix.Contains(address.Unmap())
	})
}

func VLANRefusalUnit(contextName, provider string) string {
	return "bootwright-" + contextName + "-" + provider + "-vlan-refusal.service"
}
