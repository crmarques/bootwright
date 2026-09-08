package lifecycle

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct{}

type PlanRequest struct {
	ContextName string
}

func (Service) Plan(ctx context.Context, _ PlanRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type StatusRequest struct {
	ContextName   string
	Watch         bool
	WatchInterval time.Duration
}

func (Service) Status(ctx context.Context, _ StatusRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ApplyRequest struct {
	ContextName      string
	Authorizations   []string
	SkipConfirmation bool
	Verbose          bool
	SSH              machine.SSHOptions
}

func (Service) Apply(ctx context.Context, _ ApplyRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type DestroyRequest struct {
	ContextName      string
	Authorizations   []string
	SkipConfirmation bool
	Verbose          bool
	SSH              machine.SSHOptions
}

func (Service) Destroy(ctx context.Context, _ DestroyRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
