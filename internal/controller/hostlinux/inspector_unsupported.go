//go:build !linux || !amd64

package hostlinux

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

type Inspector struct{}

func New() Inspector { return Inspector{} }

func (Inspector) Platform(ctx context.Context) (prerequisites.Platform, error) {
	return prerequisites.Platform{}, unsupported(ctx)
}

func (Inspector) Identity(ctx context.Context) (controller.InstalledHostIdentity, error) {
	return controller.InstalledHostIdentity{}, unsupported(ctx)
}

func (Inspector) Runtime(ctx context.Context, _ prerequisites.RuntimeRequirement) (prerequisites.RuntimeInspection, error) {
	return prerequisites.RuntimeInspection{}, unsupported(ctx)
}

func unsupported(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return desiredstate.NewFailure("controller.unsupported", "local host inspection requires Linux/amd64", "")
}
