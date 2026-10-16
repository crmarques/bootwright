//go:build !linux

package preflightlocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

type Observer struct{}

func (Observer) Observe(ctx context.Context, _ preflight.Target) (preflight.Facts, error) {
	return preflight.Facts{Reason: "local capacity observation is unavailable on this platform"}, ctx.Err()
}
