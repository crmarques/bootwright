//go:build !linux || !amd64

package sshlocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
)

type Copier struct {
	SSH     Launcher
	Account func(context.Context) (machineaccess.CopyAccount, error)
}

func (Copier) Prepare(context.Context, machine.Session, machineaccess.CopyRequest, bool) (machineaccess.PreparedCopy, error) {
	return nil, availability.ErrNotImplemented
}
