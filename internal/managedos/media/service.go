package media

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type AddMediaRequest struct {
	ContextName      string
	Name             string
	SourceFile       string
	SourceURL        string
	SHA256           string
	SkipConfirmation bool
}

func (Service) Add(ctx context.Context, _ AddMediaRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListMediaRequest struct {
	ContextName string
	Checksums   bool
}

func (Service) List(ctx context.Context, _ ListMediaRequest) error {
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

func (Service) Delete(ctx context.Context, _ DeleteMediaRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
