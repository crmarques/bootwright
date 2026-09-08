package installation

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type RenderInstallerRequest struct {
	ContextName string
	Clusters    []string
	Sensitive   bool
}

func (Service) Render(ctx context.Context, _ RenderInstallerRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
