package enrollment

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type EnrollRequest struct {
	ContextName      string
	Machines         []string
	Replace          []string
	DryRun           bool
	SkipConfirmation bool
}

func (Service) Enroll(ctx context.Context, _ EnrollRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
