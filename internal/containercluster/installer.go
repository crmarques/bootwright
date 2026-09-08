package containercluster

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Installer struct{}

type RenderInstallerRequest struct {
	ContextName string
	Clusters    []string
	Sensitive   bool
}

func (Installer) Render(ctx context.Context, _ RenderInstallerRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
