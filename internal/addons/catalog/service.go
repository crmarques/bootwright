package catalog

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type ListRequest struct{}

func (Service) List(ctx context.Context, _ ListRequest) error {
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

func (Service) Add(ctx context.Context, _ AddRequest) error {
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

func (Service) Delete(ctx context.Context, _ DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
