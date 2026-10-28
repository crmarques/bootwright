package libvirt

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (HostCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		claimed = append(claimed, provider.Identity())
	}
	return claimed
}

func (MachineCapability) Claimed(catalog api.Catalog) []string {
	return substrate.HostedOn(catalog, substrate.ArmLibvirt)
}
