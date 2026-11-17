package diskdiscovery

import (
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installationtest"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const testContext = installfixtures.TestContext

var (
	field             = installfixtures.Field
	metalOn           = installfixtures.MetalOn
	metalNetwork      = installfixtures.MetalNetwork
	metalCatalog      = installfixtures.MetalCatalog
	hardwareNIC       = installfixtures.HardwareNIC
	declaredInterface = installfixtures.DeclaredInterface
	labMedia          = installationtest.LabMedia
)

type volumePins struct{ pin machine.HardwareIdentity }

func (p volumePins) PinnedIdentity(string, []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	return p.pin, p.pin.Present(), nil
}

func groupIDs(groups []reconciliation.Group) []string {
	ids := []string{}
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}
