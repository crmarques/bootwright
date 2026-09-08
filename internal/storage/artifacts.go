package storage

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Artifacts struct{}

type RenderArtifactsRequest struct {
	ContextName string
	Clusters    []string
}

func (Artifacts) Render(ctx context.Context, _ RenderArtifactsRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
