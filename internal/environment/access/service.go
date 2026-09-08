package access

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct{}

type ClusterRshRequest struct {
	ContextName string
	Name        string
	Node        string
	SSH         machine.SSHOptions
}

func (Service) ClusterRsh(ctx context.Context, _ ClusterRshRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ClusterExecRequest struct {
	ContextName string
	Name        string
	Node        string
	SSH         machine.SSHOptions
	Command     []string
}

func (Service) ClusterExec(ctx context.Context, _ ClusterExecRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
