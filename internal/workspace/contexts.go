package workspace

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Contexts struct{}

type InitRequest struct {
	Name             string
	InputDirectory   string
	SkipConfirmation bool
}

func (Contexts) Init(ctx context.Context, _ InitRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type UpdateRequest struct {
	Name             string
	InputDirectory   string
	SkipConfirmation bool
}

func (Contexts) Update(ctx context.Context, _ UpdateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type UseRequest struct {
	Name string
}

func (Contexts) Use(ctx context.Context, _ UseRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListRequest struct{}

func (Contexts) List(ctx context.Context, _ ListRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type CurrentRequest struct {
	Short bool
}

func (Contexts) Current(ctx context.Context, _ CurrentRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type DeleteRequest struct {
	Name             string
	Purge            bool
	SkipConfirmation bool
	AbandonResources bool
}

func (Contexts) Delete(ctx context.Context, _ DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
