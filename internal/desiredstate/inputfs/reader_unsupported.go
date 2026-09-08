//go:build !linux || !amd64

package inputfs

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func (Reader) Read(ctx context.Context, _ []string) (desiredstate.Sources, error) {
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, err
	}
	return desiredstate.Sources{}, desiredstate.NewFailure("input.read", "desired-state input requires Linux on amd64", "")
}

func (r Reader) ReadDirectory(ctx context.Context, path string) (desiredstate.Sources, error) {
	return r.Read(ctx, []string{path})
}
