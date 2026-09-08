package reconciliation

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Lifecycle struct{}

type PlanRequest struct {
	ContextName string
}

func (Lifecycle) Plan(ctx context.Context, _ PlanRequest) error {
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

func (Lifecycle) Status(ctx context.Context, _ StatusRequest) error {
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

func (Lifecycle) Apply(ctx context.Context, _ ApplyRequest) error {
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

func (Lifecycle) Destroy(ctx context.Context, _ DestroyRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
