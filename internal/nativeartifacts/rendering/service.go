package rendering

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type RenderRequest struct {
	ContextName     string
	InputPath       string
	OutputDirectory string
	Clusters        []string
	Sensitive       bool
}

func (Service) Render(ctx context.Context, _ RenderRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
