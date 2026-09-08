package inventory

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type ListRequest struct {
	ContextName string
	Clusters    []string
	Silent      bool
}

func (Service) List(ctx context.Context, _ ListRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
