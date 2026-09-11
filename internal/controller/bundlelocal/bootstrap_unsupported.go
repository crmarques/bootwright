//go:build !linux || !amd64

package bundlelocal

import (
	"context"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func resolveBootstrapWheels(ctx context.Context, _ *projection, _ prerequisites.BootstrapDefinition, _ prerequisites.SetupEgress) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, unsupportedPlatform()
}

func qualifyResolvedProjection(_ *projection, _ prerequisites.ExecutionRequirement) error {
	return unsupportedPlatform()
}
