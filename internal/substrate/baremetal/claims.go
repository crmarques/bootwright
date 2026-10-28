package baremetal

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (MachineCapability) Claimed(catalog api.Catalog) []string {
	claimed := substrate.HostedOn(catalog, substrate.ArmBaremetal)
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmBaremetal) {
		claimed = append(claimed, provider.Identity())
	}
	return claimed
}
