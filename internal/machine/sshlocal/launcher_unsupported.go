//go:build !(linux && amd64)

package sshlocal

import (
	"context"
	"io"
	"os"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/nativeexec"
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
	Account func(context.Context) (nativeexec.Account, error)
	Scratch string
}

func ServeOperator(args []string) (bool, int) {
	return len(args) > 0 && args[0] == operatorMode, 255
}

func New(home func() (string, error), owner func() (int, error), files Files) Launcher {
	return Launcher{Home: home, Owner: owner, Files: files}
}

func (Launcher) Run(context.Context, machine.Session, io.Reader, io.Writer, io.Writer) (int, error) {
	return 0, availability.ErrNotImplemented
}

func (l Launcher) ObserveKey(ctx context.Context, _, address string, port int, _ trust.HostKey) (trust.HostKey, error) {
	return l.Observe(ctx, address, port)
}

func (Launcher) Observe(context.Context, string, int) (trust.HostKey, error) {
	return trust.HostKey{}, availability.ErrNotImplemented
}

func (Launcher) HostKeyAlgorithms(context.Context, machine.Session) (string, error) {
	return "", availability.ErrNotImplemented
}

func (Launcher) IdentityFile(context.Context, string) (string, error) {
	return "", availability.ErrNotImplemented
}
