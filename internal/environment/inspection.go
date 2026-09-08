package environment

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
)

type Inspection struct{}

type InfrastructurePreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	Verbose         bool
	SSH             machine.SSHOptions
}

func (Inspection) PreflightInfrastructure(ctx context.Context, _ InfrastructurePreflightRequest) error {
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
	Verbose         bool
	SSH             machine.SSHOptions
}

func (Inspection) PreflightClusters(ctx context.Context, _ ClustersPreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type AllPreflightRequest struct {
	ContextName     string
	DryRun          bool
	TrustOnFirstUse bool
	Verbose         bool
	SSH             machine.SSHOptions
}

func (Inspection) PreflightAll(ctx context.Context, _ AllPreflightRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListClustersRequest struct {
	ContextName string
}

func (Inspection) ListClusters(ctx context.Context, _ ListClustersRequest) error {
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

func (Inspection) ClusterInfo(ctx context.Context, _ ClusterInfoRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ClusterRshRequest struct {
	ContextName string
	Name        string
	Node        string
	SSH         machine.SSHOptions
}

func (Inspection) ClusterRsh(ctx context.Context, _ ClusterRshRequest) error {
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

func (Inspection) ClusterExec(ctx context.Context, _ ClusterExecRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
