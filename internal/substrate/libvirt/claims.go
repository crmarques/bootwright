package libvirt

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (HostCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		claimed = append(claimed, provider.Identity())
		for _, managed := range substrate.ManagedNetworksOf(catalog, provider) {
			claimed = append(claimed, managed.Identity())
		}
	}
	return claimed
}

func (HostCapability) Carries(catalog api.Catalog) map[string][]string {
	carried := map[string][]string{}
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		for _, managed := range substrate.ManagedNetworksOf(catalog, provider) {
			carried[provider.Identity()] = append(carried[provider.Identity()], managed.Identity())
		}
	}
	return carried
}

func (MachineCapability) Claimed(catalog api.Catalog) []string {
	return substrate.HostedOn(catalog, substrate.ArmLibvirt)
}
