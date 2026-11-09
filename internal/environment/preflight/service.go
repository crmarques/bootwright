package preflight

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment/clusterpreflight"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Service struct {
	Graph      Graph
	Selection  func(context.Context) (string, error)
	Controller ControllerChecker
	Capacity   CapacityChecker
	Clusters   ClustersChecker
}

const listenerCheck = "controller.listener-firewall"

func CheckedStages() reconciliation.StageSelection {
	return slices.DeleteFunc(reconciliation.Stages(), func(stage reconciliation.Stage) bool { return stage == reconciliation.StageAddOns })
}

func (s Service) Preflight(ctx context.Context, request PreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Controller == nil || s.Capacity == nil || s.Clusters == nil {
		return nil, availability.ErrNotImplemented
	}
	stages, err := reconciliation.ParseStages(request.Stages)
	if err != nil {
		return nil, diagnostics.NewFailureWithRemediation("cli.usage", "--stage names a stage that does not exist", "", "name stages from bootwright help preflight")
	}
	if len(stages) == 0 {
		stages = CheckedStages()
	}
	if slices.Contains(stages, reconciliation.StageAddOns) {
		return nil, availability.ErrNotImplemented
	}
	clusters := slices.Contains(stages, reconciliation.StageClusters)
	if len(request.Clusters) != 0 && !clusters {
		return nil, diagnostics.NewFailureWithRemediation("cli.usage", "--clusters selects clusters, which only the clusters stage checks", "", "add clusters to --stage, or omit --clusters")
	}
	selected, err := machine.SelectedContext(ctx, s.Selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	catalog, err := s.Graph.Effective(ctx, selected)
	if err != nil {
		return nil, err
	}
	if err := clusterpreflight.RefuseUnreachedSSH(request.SSH, clusters && len(catalog.OfCategory(api.StorageClusterCategory)) > 0); err != nil {
		return nil, err
	}
	var parts []clusterpreflight.Part
	if slices.Contains(stages, reconciliation.StageController) || slices.Contains(stages, reconciliation.StageInfrastructureServices) {
		parts = append(parts, func(checks context.Context) (*Report, error) {
			return s.controllerReport(checks, selected, request.DryRun, stages)
		})
	}
	if slices.Contains(stages, reconciliation.StageSubstrates) {
		parts = append(parts, func(checks context.Context) (*Report, error) {
			return s.capacityReport(checks, catalog, selected, request.DryRun)
		})
	}
	if clusters {
		parts = append(parts, func(checks context.Context) (*Report, error) {
			return s.Clusters.Check(checks, clusterpreflight.PreflightRequest{ContextName: selected, Clusters: request.Clusters, DryRun: request.DryRun, TrustOnFirstUse: request.TrustOnFirstUse, SSH: request.SSH})
		})
	}
	reports, err := clusterpreflight.RunTogether(ctx, parts...)
	if err != nil {
		return nil, err
	}
	return clusterpreflight.Assemble(selected, stageList(stages), repeatCommand(selected, request.Clusters, stages, len(request.Stages) != 0), reports...)
}

func stageList(stages reconciliation.StageSelection) string {
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, string(stage))
	}
	return strings.Join(names, ",")
}

func repeatCommand(selected string, clusters []string, stages reconciliation.StageSelection, explicit bool) string {
	command := "bootwright preflight --context " + selected
	if explicit {
		command += " --stage " + stageList(stages)
	}
	if len(clusters) != 0 {
		command += " --clusters " + strings.Join(clusters, ",")
	}
	return command
}

func (s Service) controllerReport(ctx context.Context, selected string, dryRun bool, stages reconciliation.StageSelection) (*Report, error) {
	controller := slices.Contains(stages, reconciliation.StageController)
	listeners := slices.Contains(stages, reconciliation.StageInfrastructureServices)
	if dryRun {
		var checks []capacity.Check
		if controller {
			checks = append(checks, capacity.Check{ID: "controller.readiness", Target: "controller", Status: capacity.Pending, Reason: "dry-run would check the controller's host and context prerequisites", Next: "repeat without --dry-run"})
		}
		if listeners {
			checks = append(checks, capacity.Check{ID: listenerCheck, Target: "controller", Status: capacity.Pending, Reason: "dry-run would check the firewall admission of the controller's managed service sockets", Next: "repeat without --dry-run"})
		}
		return &Report{Checks: checks}, nil
	}
	report, err := s.Controller.Check(ctx, prerequisites.CheckRequest{ContextName: selected})
	if report == nil {
		return nil, err
	}
	checks := controllerChecks(report)
	if err != nil && !anyUnready(checks) {
		return nil, err
	}
	return &Report{Checks: slices.DeleteFunc(checks, func(check capacity.Check) bool {
		if check.ID == listenerCheck {
			return !listeners
		}
		return !controller
	})}, nil
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
