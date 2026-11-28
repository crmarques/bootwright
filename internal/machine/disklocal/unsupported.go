//go:build !linux || !amd64

package disklocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine/inventory"
)

func Survey(context.Context, bool) (inventory.DiskSnapshot, error) {
	return inventory.DiskSnapshot{}, availability.ErrNotImplemented
}
