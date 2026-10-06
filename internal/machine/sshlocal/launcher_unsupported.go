//go:build !(linux && amd64)

package sshlocal

import (
	"context"
	"io"
	"os"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

type Files interface {
	Begin(context.Context) (FileSession, error)
}

type FileSession interface {
	OpenFile(string) (*os.File, error)
	Close() error
}

type Launcher struct {
	Client  string
	Home    func() (string, error)
	Owner   func() (int, error)
	Files   Files
	Scratch string
}

func New(home func() (string, error), owner func() (int, error), files Files) Launcher {
	return Launcher{Home: home, Owner: owner, Files: files}
}

func (Launcher) Run(context.Context, machine.Session, io.Reader, io.Writer, io.Writer) (int, error) {
	return 0, availability.ErrNotImplemented
}

func (Launcher) Observe(context.Context, string, int) (trust.HostKey, error) {
	return trust.HostKey{}, availability.ErrNotImplemented
}

func (Launcher) IdentityFile(context.Context, string) (string, error) {
	return "", availability.ErrNotImplemented
}
