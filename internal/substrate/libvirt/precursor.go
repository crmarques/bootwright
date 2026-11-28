package libvirt

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (HostCapability) PrecedesController(reconciliation.BlockDefinition) bool { return true }
func (MachineCapability) PrecedesController(reconciliation.BlockDefinition) bool {
	return substrate.MachinePrecedesController(substrate.ArmLibvirt)
}
