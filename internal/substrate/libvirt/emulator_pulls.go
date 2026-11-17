package libvirt

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/substrate"
)

type EmulatorPull struct {
	Origin, Pulled, Host, Provider string
}

func EmulatorPulls(catalog api.Catalog) []EmulatorPull {
	var pulls []EmulatorPull
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if substrate.Variant(provider) != substrate.ArmLibvirt {
			continue
		}
		host := substrate.HostMachine(provider)
		var pulled []string
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			reference, _ := managedservice.MirroredImage(catalog, emulatorImage, managedservice.EmulatedBMCConsumer(machine, host))
			if !slices.Contains(pulled, reference) {
				pulled = append(pulled, reference)
				pulls = append(pulls, EmulatorPull{Origin: emulatorImage, Pulled: reference, Host: host, Provider: provider.Identity()})
			}
		}
	}
	return pulls
}
