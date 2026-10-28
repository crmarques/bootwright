package preflight

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/environment/clusterpreflight"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Service struct {
	Graph      Graph
	Selection  func(context.Context) (string, error)
	Controller ControllerChecker
	Capacity   CapacityChecker
	Clusters   ClustersChecker
}

func (s Service) PreflightInfrastructure(ctx context.Context, request InfrastructurePreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Controller == nil || s.Capacity == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.Selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	catalog, err := s.Graph.Effective(ctx, selected)
	if err != nil {
		return nil, err
	}
	return s.infrastructure(ctx, selected, catalog, request.DryRun)
}

func (s Service) infrastructure(ctx context.Context, selected string, catalog api.Catalog, dryRun bool) (*Report, error) {
	reports, err := clusterpreflight.RunTogether(ctx,
		func(checks context.Context) (*Report, error) { return s.controllerReport(checks, selected, dryRun) },
		func(checks context.Context) (*Report, error) {
			return s.capacityReport(checks, catalog, selected, dryRun)
		},
	)
	if err != nil {
		return nil, err
	}
	return clusterpreflight.Assemble(selected, "infra", "infrastructure", "infra", reports...)
}

func (s Service) PreflightAll(ctx context.Context, request AllPreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Controller == nil || s.Capacity == nil || s.Clusters == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.Selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	catalog, err := s.Graph.Effective(ctx, selected)
	if err != nil {
		return nil, err
	}
	if err := clusterpreflight.RefuseUnreachedSSH(request.SSH, len(catalog.OfCategory(api.StorageClusterCategory)) > 0); err != nil {
		return nil, err
	}
	reports, err := clusterpreflight.RunTogether(ctx,
		func(checks context.Context) (*Report, error) {
			report, err := s.infrastructure(checks, selected, catalog, request.DryRun)
			if report != nil {
				return report, nil
			}
			return nil, err
		},
		func(checks context.Context) (*Report, error) {
			return s.Clusters.Check(checks, clusterpreflight.PreflightRequest{ContextName: selected, DryRun: request.DryRun, TrustOnFirstUse: request.TrustOnFirstUse, SSH: request.SSH})
		},
	)
	if err != nil {
		return nil, err
	}
	return clusterpreflight.Assemble(selected, "all", "environment", "all", reports...)
}

func (s Service) controllerReport(ctx context.Context, selected string, dryRun bool) (*Report, error) {
	if dryRun {
		return &Report{Checks: []capacity.Check{{ID: "controller.readiness", Target: "controller", Status: capacity.Pending, Reason: "dry-run would check the controller's host, context and listener prerequisites", Next: "repeat without --dry-run"}}}, nil
	}
	report, err := s.Controller.Check(ctx, prerequisites.CheckRequest{ContextName: selected})
	if report == nil {
		return nil, err
	}
	checks := controllerChecks(report)
	if err != nil && !anyUnready(checks) {
		return nil, err
	}
	return &Report{Checks: checks}, nil
}

func (s Service) capacityReport(ctx context.Context, catalog api.Catalog, selected string, dryRun bool) (*Report, error) {
	checks, err := s.Capacity.CheckCapacity(ctx, catalog, selected, dryRun)
	if err != nil {
		return nil, err
	}
	if dryRun {
		for index := range checks {
			checks[index].Status = capacity.Pending
			checks[index].Reason = "dry-run would check " + checks[index].ID + " on " + checks[index].Target
		}
	}
	return &Report{Checks: checks}, nil
}

func controllerChecks(report *prerequisites.Report) []capacity.Check {
	checks := make([]capacity.Check, 0, len(report.Checks))
	for _, row := range report.Checks {
		check := capacity.Check{ID: "controller." + row.ID, Target: "controller", Reason: row.Summary()}
		if row.Target != "" {
			check.Target = row.Target
		}
		switch row.Status {
		case "ready":
			check.Status = capacity.Passed
		case "not-ready":
			check.Status = capacity.Failed
		default:
			check.Status = capacity.Unknown
		}
		if check.Status != capacity.Passed {
			check.Next = "resolve the reported prerequisite and repeat the preflight"
			if report.Next != "" {
				check.Next = "run " + report.Next
			}
		}
		checks = append(checks, check)
	}
	return checks
}

func anyUnready(checks []capacity.Check) bool {
	for _, check := range checks {
		if check.Status != capacity.Passed {
			return true
		}
	}
	return false
}
