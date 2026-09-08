package nativeartifacts

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Renderer struct{}

type RenderRequest struct {
	ContextName     string
	InputPath       string
	OutputDirectory string
	Clusters        []string
	Sensitive       bool
}

func (Renderer) Render(ctx context.Context, _ RenderRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
