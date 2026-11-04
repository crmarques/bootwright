//go:build !linux

package preflightlocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

type Observer struct{ Remote RemoteCommands }

func (o Observer) Observe(ctx context.Context, target preflight.Target) (preflight.Facts, error) {
	if !target.Local && o.Remote != nil {
		return observeRemote(ctx, o.Remote, target)
	}
	return preflight.Facts{Reason: "local capacity observation is unavailable on this platform"}, ctx.Err()
}
