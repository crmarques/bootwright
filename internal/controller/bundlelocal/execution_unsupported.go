//go:build !linux || !amd64

package bundlelocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type ExecutionGuard struct{}

func (ExecutionGuard) WithPython(ctx context.Context, _ prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, _ func(prerequisites.PythonLaunch, func() error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return unsupportedPlatform()
}

func (ExecutionGuard) Inspect(ctx context.Context, _ prerequisites.Platform) (prerequisites.FoundationInspection, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.FoundationInspection{}, err
	}
	return prerequisites.FoundationInspection{}, unsupportedPlatform()
}
