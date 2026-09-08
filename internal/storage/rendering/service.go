package rendering

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type RenderArtifactsRequest struct {
	ContextName string
	Clusters    []string
}

func (Service) Render(ctx context.Context, _ RenderArtifactsRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
