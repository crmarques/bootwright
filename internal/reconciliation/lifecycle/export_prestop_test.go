package lifecycle

import "github.com/crmarques/bootwright/internal/reconciliation"

func (p plannedCapability) RequiresPreStopRemoval(block reconciliation.Block) bool {
	removal, ok := p.Capability.(PreStopRemoval)
	return ok && removal.RequiresPreStopRemoval(block)
}
