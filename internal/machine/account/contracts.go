package account

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Launcher interface {
	Run(context.Context, machine.Session, io.Reader, io.Writer, io.Writer) (int, error)
	IdentityFile(context.Context, string) (string, error)
}

type remoteAccount interface {
	manage(context.Context, string, Request, string, string, map[string]secrets.Material) (string, error)
	prove(context.Context, string, Request, map[string]secrets.Material) error
}
