package libvirt

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
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

// SharedClaims is the bridge-use claim a frozen provider-host block plans.
func (HostCapability) SharedClaims(block reconciliation.Block) ([]infrastructureservices.HostReservation, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	shared := request.SharedReservationKeys()
	if len(shared) == 0 {
		return nil, nil
	}
	return []infrastructureservices.HostReservation{{Kind: "substrate-host-use", Service: request.Identity.Object, Keys: shared, Shared: true}}, nil
}

// SharedClaims is the emulated-BMC claim a frozen machine block plans.
func (MachineCapability) SharedClaims(block reconciliation.Block) ([]infrastructureservices.HostReservation, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return []infrastructureservices.HostReservation{{Kind: "substrate-machine-bmc", Service: request.Identity.Object, Keys: request.SharedReservationKeys(), Shared: true}}, nil
}
