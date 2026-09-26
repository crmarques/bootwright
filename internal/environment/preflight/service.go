package preflight

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct{}

type InfrastructurePreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

func (Service) PreflightInfrastructure(ctx context.Context, _ InfrastructurePreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ClustersPreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

func (Service) PreflightClusters(ctx context.Context, _ ClustersPreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type AllPreflightRequest struct {
	ContextName     string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

func (Service) PreflightAll(ctx context.Context, _ AllPreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
