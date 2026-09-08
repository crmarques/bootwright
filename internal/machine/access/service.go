package access

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct{}

type RshRequest struct {
	ContextName string
	Name        string
	SSH         machine.SSHOptions
}

func (Service) Rsh(ctx context.Context, _ RshRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ExecRequest struct {
	ContextName string
	Name        string
	SSH         machine.SSHOptions
	Command     []string
}

func (Service) Exec(ctx context.Context, _ ExecRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
