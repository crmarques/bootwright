package managedos

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Media struct{}

type AddMediaRequest struct {
	ContextName      string
	Name             string
	SourceFile       string
	SourceURL        string
	SHA256           string
	SkipConfirmation bool
}

func (Media) Add(ctx context.Context, _ AddMediaRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListMediaRequest struct {
	ContextName string
	Checksums   bool
}

func (Media) List(ctx context.Context, _ ListMediaRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type DeleteMediaRequest struct {
	ContextName      string
	Name             string
	SkipConfirmation bool
}

func (Media) Delete(ctx context.Context, _ DeleteMediaRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
