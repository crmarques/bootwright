//go:build !(linux && amd64)

package sshlocal

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

type Launcher struct {
	Client  string
	Home    func() (string, error)
	Scratch string
}

func New(home func() (string, error)) Launcher { return Launcher{Home: home} }

func (Launcher) Run(context.Context, machine.Session, io.Reader, io.Writer, io.Writer) (int, error) {
	return 0, availability.ErrNotImplemented
}

func (Launcher) Observe(context.Context, string, int) (trust.HostKey, error) {
	return trust.HostKey{}, availability.ErrNotImplemented
}

func (Launcher) IdentityFile(string) (string, error) { return "", availability.ErrNotImplemented }
