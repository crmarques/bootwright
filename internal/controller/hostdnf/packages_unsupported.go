//go:build !linux || !amd64

package hostdnf

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Packages struct{}

var _ prerequisites.HostPackages = (*Packages)(nil)

func New(controller.Route) (*Packages, error) { return &Packages{}, nil }

func ForEgress(prerequisites.SetupEgress) (*Packages, error) { return &Packages{}, nil }

func (*Packages) Qualify(ctx context.Context) error { return unsupported(ctx) }

func (*Packages) Present(ctx context.Context, _ []string) ([]prerequisites.HostPackageEvidence, error) {
	return nil, unsupported(ctx)
}

func (*Packages) Installed(ctx context.Context, _ []string) ([]prerequisites.HostPackageEvidence, error) {
	return nil, unsupported(ctx)
}

func (*Packages) Missing(ctx context.Context, _ []string) ([]string, error) {
	return nil, unsupported(ctx)
}

func (*Packages) Preview(ctx context.Context, _ []string) (prerequisites.HostPackagePreview, error) {
	return prerequisites.HostPackagePreview{}, unsupported(ctx)
}

func (*Packages) Install(ctx context.Context, _ []string, _ prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	return nil, unsupported(ctx)
}

func unsupported(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return diagnostics.NewFailure("controller.unsupported", "host package installation requires Linux/amd64", "")
}
