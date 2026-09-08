package inspection

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type ListClustersRequest struct {
	ContextName string
}

func (Service) ListClusters(ctx context.Context, _ ListClustersRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ClusterInfoRequest struct {
	ContextName string
	Name        string
	Secrets     bool
}

func (Service) ClusterInfo(ctx context.Context, _ ClusterInfoRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
