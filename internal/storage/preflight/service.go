package preflight

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct{}

type PreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	Verbose         bool
	SSH             machine.SSHOptions
}

func (Service) Check(ctx context.Context, _ PreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
