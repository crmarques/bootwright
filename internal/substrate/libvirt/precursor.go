package libvirt

import "github.com/crmarques/bootwright/internal/reconciliation"

func (HostCapability) PrecedesController(reconciliation.BlockDefinition) bool    { return true }
func (MachineCapability) PrecedesController(reconciliation.BlockDefinition) bool { return true }
