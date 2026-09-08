package access

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type OCRequest struct {
	ContextName string
	Name        string
	Command     []string
}

func (Service) OC(ctx context.Context, _ OCRequest) error {
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

func (Service) Kubectl(ctx context.Context, _ KubectlRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type KubeconfigRequest struct {
	ContextName string
	Name        string
}

func (Service) Kubeconfig(ctx context.Context, _ KubeconfigRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
