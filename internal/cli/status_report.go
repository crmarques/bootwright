package cli

import (
	"io"
	"time"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

type statusReport = status.Report

func runStatusJSON(stdout io.Writer, cf *commonFlags) error {
	report, blocking, err := buildStatusReport(cf)
	if err != nil {
		return failErr(1, err)
	}
	if err := output.JSON(stdout, report); err != nil {
		return failErr(1, err)
	}
	if blocking {
		return silentExit(1)
	}
	return nil
}

func buildStatusReport(cf *commonFlags) (status.Report, bool, error) {
	ctx, err := cf.resolve()
	if err != nil {
		// Context missing or not ready: report the setup checks (the surface
		// that used to be `context validate`) instead of failing opaquely.
		vctx, checks := currentContextValidation()
		report := status.Report{
			Context: status.Context{
				Name:               vctx.Name,
				ContextDir:         vctx.BaseDir,
				InputDir:           vctx.InputDir,
				RenderedDir:        vctx.RenderedDir,
				ClustersDir:        vctx.ClustersDir,
				RunsDir:            vctx.RunsDir,
				ManagedServicesDir: vctx.ManagedServicesDir,
				ProviderStateDir:   vctx.ProviderStateDir,
				SecretsDir:         vctx.SecretsDir,
			},
			Error:           err.Error(),
			SetupChecks:     contextValidateChecks(checks),
			Clusters:        []status.Cluster{},
			StorageClusters: []status.StorageCluster{},
			Shared:          []status.Shared{},
			NextSteps:       contextValidateNextSteps(checks),
		}
		return report, true, nil
	}
	state, loadErr := loadOptionalDesiredState(cf)
	stateLoaded := loadErr == nil && status.HasAnyState(state)

	report := status.Report{
		Context: status.Context{
			Name:               ctx.Name,
			ContextDir:         ctx.BaseDir,
			InputDir:           ctx.InputDir,
			RenderedDir:        ctx.RenderedDir,
			ClustersDir:        ctx.ClustersDir,
			RunsDir:            ctx.RunsDir,
			ManagedServicesDir: ctx.ManagedServicesDir,
			ProviderStateDir:   ctx.ProviderStateDir,
			SecretsDir:         ctx.SecretsDir,
		},
		Desired: status.Desired{
			Source: stateSource(cf),
			Loaded: stateLoaded,
		},
		Clusters:        []status.Cluster{},
		StorageClusters: []status.StorageCluster{},
		Shared:          []status.Shared{},
		NextSteps:       nextStepHints(stateLoaded, state, ctx.RenderedDir, ctx.ClustersDir, ctx.Name, ctx.SecretsDir),
	}
	setupChecks := contextReadinessChecks(ctx)
	if loadErr != nil {
		report.Desired.LoadError = loadErr.Error()
	}
	if stateLoaded {
		setupChecks = append(setupChecks, contextHostTrustChecks(ctx.BaseDir, state)...)
		setupChecks = append(setupChecks, bastionLocalityCheck(state))
		report.Desired.Environments = len(state.Environments)
		report.Desired.Machines = len(state.Machines)
		report.Desired.MachineImages = len(state.MachineImages)
		report.Desired.MachineInstallProfiles = len(state.MachineInstallProfiles)
		report.Desired.InfraProviders = len(state.InfraProviders)
		report.Desired.ContainerClusters = len(state.ContainerClusters)
		report.Desired.StorageClusters = len(state.StorageClusters)
		report.Desired.StoragePools = len(state.StoragePools)
		report.Desired.ClusterAddons = len(state.ClusterAddons)
		report.Desired.ClusterAddonProfiles = len(state.ClusterAddonProfiles)
		report.Desired.ClusterAddonBindings = len(state.ClusterAddonBindings)
		report.Clusters = status.BuildClusters(state, ctx.RenderedDir, ctx.ClustersDir)
		report.StorageClusters = status.BuildStorageClusters(state)
		report.Shared = status.BuildShared(state)
		report.Secrets, _ = declaredSecretEntriesForContext(ctx.Name, ctx.SecretsDir, state)
	}
	report.SetupChecks = contextValidateChecks(setupChecks)
	if ledger, ok, err := workflow.LoadRunLedger(ctx.RunsDir); err == nil && ok {
		report.ApplyRun = &ledger
		activity, _ := workflow.AssessRunActivity(ctx.RunsDir, ledger, time.Now())
		report.ApplyRunActivity = &status.ApplyRunActivity{State: string(activity.State), Detail: activity.Detail}
		report.NextSteps = status.LedgerNextSteps(ledger, activity, report.NextSteps)
	} else if err != nil {
		report.NextSteps = append([]string{"inspect apply ledger: " + err.Error()}, report.NextSteps...)
	}
	return report, false, nil
}
