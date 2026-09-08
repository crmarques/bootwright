package containercluster

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Access struct{}

type OCRequest struct {
	ContextName string
	Name        string
	Command     []string
}

func (Access) OC(ctx context.Context, _ OCRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type KubectlRequest struct {
	ContextName string
	Name        string
	Command     []string
}

func (Access) Kubectl(ctx context.Context, _ KubectlRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type KubeconfigRequest struct {
	ContextName string
	Name        string
}

func (Access) Kubeconfig(ctx context.Context, _ KubeconfigRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
