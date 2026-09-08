package trust

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Machines struct{}

type EnrollRequest struct {
	ContextName      string
	Machines         []string
	Replace          []string
	DryRun           bool
	SkipConfirmation bool
}

func (Machines) Enroll(ctx context.Context, _ EnrollRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
