package controller

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Prerequisites struct{}

type CheckRequest struct{}

func (Prerequisites) Check(ctx context.Context, _ CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type SetupRequest struct {
	DryRun           bool
	SkipConfirmation bool
}

func (Prerequisites) Setup(ctx context.Context, _ SetupRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
