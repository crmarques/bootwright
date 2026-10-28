package preflight

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/environment/clusterpreflight"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Report = clusterpreflight.Report

type InfrastructurePreflightRequest struct {
	ContextName string
	DryRun      bool
}

type AllPreflightRequest struct {
	ContextName     string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

type Graph interface {
	Effective(context.Context, string) (api.Catalog, error)
}

type ControllerChecker interface {
	Check(context.Context, prerequisites.CheckRequest) (*prerequisites.Report, error)
}

type CapacityChecker interface {
	CheckCapacity(context.Context, api.Catalog, string, bool) ([]capacity.Check, error)
}

type ClustersChecker interface {
	Check(context.Context, clusterpreflight.PreflightRequest) (*Report, error)
}
