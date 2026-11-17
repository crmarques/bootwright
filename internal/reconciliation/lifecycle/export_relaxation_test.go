package lifecycle

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (p plannedCapability) RelaxesMachine(block reconciliation.Block) bool {
	relaxer, ok := p.Capability.(RemovalRelaxer)
	return ok && relaxer.RelaxesMachine(block)
}

func (p plannedCapability) Relax(ctx context.Context, block reconciliation.Block, method string) (Removal, error) {
	relaxer, ok := p.Capability.(RemovalRelaxer)
	if !ok {
		return Removal{}, errors.New("the journey capability relaxes no removal")
	}
	return relaxer.Relax(ctx, block, method)
}
