package main

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type fipsWiringHost struct {
	prerequisites.HostInspector
	enabled bool
}

func (h fipsWiringHost) FIPSMode(context.Context) (bool, error) { return h.enabled, nil }

func TestFIPSHostRefusesBeforePlanEffectsAndApplyClaims(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.ContainerCluster, "fixture", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "security", "fips", "enabled"))})
	for _, enabled := range []bool{false, true} {
		host := fipsWiringHost{enabled: enabled}
		planCalls, applyCalls := 0, 0
		inspect := fipsPlanInspection(host, func(context.Context, string, api.Catalog, reconciliation.Plan, []reconciliation.Block) ([]lifecycle.PlanCheck, error) {
			planCalls++
			return nil, nil
		})
		admit := fipsApplyAdmission(host, func(context.Context, string, api.Catalog, map[string]lifecycle.ObjectOwnership, []reconciliation.Block) error {
			applyCalls++
			return nil
		})
		_, planErr := inspect(t.Context(), "fixture", catalog, reconciliation.Plan{}, nil)
		applyErr := admit(t.Context(), "fixture", catalog, nil, nil)
		if enabled && (planErr != nil || applyErr != nil || planCalls != 1 || applyCalls != 1) {
			t.Fatalf("enabled host refused: %v %v", planErr, applyErr)
		}
		if !enabled && (planErr == nil || applyErr == nil || planCalls != 0 || applyCalls != 0) {
			t.Fatalf("disabled host crossed effect boundary: %v %v %d %d", planErr, applyErr, planCalls, applyCalls)
		}
	}
}
