package addons

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Catalog struct{}

type ListRequest struct{}

func (Catalog) List(ctx context.Context, _ ListRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type AddRequest struct {
	Name             string
	Version          string
	SkipConfirmation bool
}

func (Catalog) Add(ctx context.Context, _ AddRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type DeleteRequest struct {
	Name             string
	Version          string
	SkipConfirmation bool
}

func (Catalog) Delete(ctx context.Context, _ DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
