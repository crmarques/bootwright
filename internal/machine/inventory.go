package machine

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Inventory struct{}

type ListRequest struct {
	ContextName string
	Clusters    []string
	Silent      bool
}

func (Inventory) List(ctx context.Context, _ ListRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
