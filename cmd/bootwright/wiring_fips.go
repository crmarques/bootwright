package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func fipsPlanInspection(host prerequisites.HostInspector, next lifecycle.PlanInspection) lifecycle.PlanInspection {
	inspector, _ := host.(prerequisites.FIPSInspector)
	return func(ctx context.Context, name string, catalog api.Catalog, plan reconciliation.Plan, starting []reconciliation.Block) ([]lifecycle.PlanCheck, error) {
		if err := prerequisites.CheckClusterFIPS(ctx, inspector, catalog); err != nil {
			return nil, err
		}
		return next(ctx, name, catalog, plan, starting)
	}
}

func fipsApplyAdmission(host prerequisites.HostInspector, next lifecycle.ApplyAdmission) lifecycle.ApplyAdmission {
	inspector, _ := host.(prerequisites.FIPSInspector)
	return func(ctx context.Context, name string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block) error {
		if err := prerequisites.CheckClusterFIPS(ctx, inspector, catalog); err != nil {
			return err
		}
		return next(ctx, name, catalog, owned, starting)
	}
}
