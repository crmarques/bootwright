package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

// PrecedesController holds for an installation whose substrate arm runs no
// controller client: its media is built and published on the controller with
// the stage's host packages and its completion is read through the hypervisor
// or a pinned connection, so a mirror host it installs may precede the stage's
// clients. An arm that runs one, such as govc or virtctl, waits for them.
func (Capability) PrecedesController(definition reconciliation.BlockDefinition) bool {
	request, err := DecodeRequest(definition.Request)
	return err == nil && len(substrate.ClientsOf(request.Target.Substrate)) == 0
}

// PrecedesController holds for a package tree, which is extracted on the
// controller with the stage's host packages and published into an artifact
// server there, running no controller client.
func (TreeCapability) PrecedesController(reconciliation.BlockDefinition) bool { return true }
