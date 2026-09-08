package machine

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Access struct{}

type RshRequest struct {
	ContextName string
	Name        string
	SSH         SSHOptions
}

func (Access) Rsh(ctx context.Context, _ RshRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ExecRequest struct {
	ContextName string
	Name        string
	SSH         SSHOptions
	Command     []string
}

func (Access) Exec(ctx context.Context, _ ExecRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
