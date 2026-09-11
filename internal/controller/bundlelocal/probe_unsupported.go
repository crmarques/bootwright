//go:build !linux || !amd64

package bundlelocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func probeBundle(ctx context.Context, _ prerequisites.BundleArea, _ prerequisites.Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return unsupportedPlatform()
}
