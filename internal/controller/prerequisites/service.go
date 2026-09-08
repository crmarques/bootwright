package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type CheckRequest struct{}

func (Service) Check(ctx context.Context, _ CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type SetupRequest struct {
	DryRun           bool
	SkipConfirmation bool
}

func (Service) Setup(ctx context.Context, _ SetupRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
