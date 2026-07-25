package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/workspace"
)

type scopeApplyOptions struct {
	use           string
	short         string
	long          string
	example       string
	defaultPlan   bool
	hideDryRun    bool
	hideApproval  bool
	hideExecFlags bool
	stageSelector bool
	commandLabel  string
	action        string
}

func newScopeApplyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeApplyOptions) *cobra.Command {
	usesAnsible := converge.ScopeUsesAnsible(scope)
	var (
		flags           scopeCommonFlags
		dryRun          = options.defaultPlan
		askBecomePass   bool
		yes             bool
		override        bool
		allowDestroy    bool
		expectNew       bool
		trustOnFirstUse bool
		verbose         bool
		stage           string
		through         string
		reclaimDevices  string
		machinesScope   string
	)
	use := "apply"
	if options.use != "" {
		use = options.use
	}
	short := "Apply " + scope.Name + " desired state"
	if options.short != "" {
		short = options.short
	}
	example := ""
	if options.example != "" {
		example = options.example
	}
	commandLabel := scope.Name + " apply"
	if options.commandLabel != "" {
		commandLabel = options.commandLabel
	}
	action := "apply"
	if options.action != "" {
		action = options.action
	}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    options.long,
		Args:    cobra.NoArgs,
		Example: example,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", options.defaultPlan, flagDryRunUsage)
	if options.hideDryRun {
		_ = cmd.Flags().MarkHidden("dry-run")
	}
	if usesAnsible {
		addAskBecomePassFlag(cmd, &askBecomePass)
	}
	if !options.hideApproval {
		addYesFlag(cmd, &yes, action)
		addTrustOnFirstUseFlag(cmd, &trustOnFirstUse)
	}
	addVerboseFlag(cmd, &verbose)
	cmd.Flags().BoolVar(&expectNew, "expect-new", false, "assert a greenfield run: fail if any selected object already exists; without it apply reconciles (creates what is missing, skips what matches, fails closed on drift)")
	if converge.ScopeTargetsContainerInstall(scope) {
		cmd.Flags().BoolVar(&override, "converge-drifted", false, "authorize Bootwright-owned destructive rebuilds (rebuild drifted owned objects, managed-OS VM reinstall, owned-Ceph wipe-and-rebuild); never touches foreign objects, and skips objects already matching desired state; mutually exclusive with --expect-new")
		cmd.Flags().BoolVar(&allowDestroy, "confirm-data-loss", false, "authorize a destructive --converge-drifted rebuild (machine reinstall with disks wiped, Ceph OSD zap) — required alongside --yes for such a rebuild, and pre-accepts the interactive data-loss prompt; --yes alone never authorizes data loss")
	}
	if usesAnsible {
		cmd.Flags().StringVar(&reclaimDevices, "reclaim-devices", "", "comma-separated block-device paths to WIPE in-band before a managed-Ceph apply (recover owned OSD disks whose on-node marker was lost by a managed-OS reinstall); only wipes a named device that is a declared OSD device of a Bootwright-owned cluster, is not mounted or a system disk, and is on a host whose OSD marker does not already record it — irreversible data loss")
	}
	if options.stageSelector {
		if usesAnsible {
			flags.executable = workspace.ResolveAnsiblePlaybook()
		}
		addOutputFlagDryRun(cmd, &flags.output)
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("first stage to %s: %s (or sub-phase %s); with --through it is the start of an inclusive range, otherwise the only stage; default: full graph", action, strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.ApplyStageNames())
		cmd.Flags().StringVar(&through, "through", "", fmt.Sprintf("last stage to %s (inclusive): %s (or sub-phase %s, or 'end' for the final stage); pairs with --stage as the range end, otherwise runs from the first stage", action, strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerFlagCompletion(cmd, "through", converge.ApplyThroughNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to apply (default: all)")
		registerClusterScopeCompletion(cmd, clusterKindAny)
		cmd.Flags().StringVar(&machinesScope, "machines", "", flagMachinesUsage)
		registerMachineScopeCompletion(cmd)
	} else {
		registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), action, usesAnsible, scopeTargetKind(scope))
	}
	if options.defaultPlan {
		if flag := cmd.Flags().Lookup("output"); flag != nil {
			flag.Usage = flagOutputUsage
		}
	}
	if options.hideExecFlags {
		for _, name := range []string{"reclaim-devices", "confirm-data-loss", "ask-become-pass", "verbose"} {
			_ = cmd.Flags().MarkHidden(name)
		}
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		if options.defaultPlan && !dryRun {
			return failErr(2, errors.New("plan is always read-only"))
		}
		if expectNew && override {
			return failErr(2, errors.New("--expect-new and --converge-drifted are mutually exclusive: --expect-new asserts nothing exists yet, --converge-drifted rebuilds drift"))
		}
		mode := workflow.ApplyModeContinue
		switch {
		case override:
			mode = workflow.ApplyModeOverride
		case expectNew:
			mode = workflow.ApplyModeCreate
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			var err error
			switch {
			case stage != "" && through != "":
				runScope, err = converge.ApplyRangeScope(stage, through)
				runCommandLabel = converge.ApplyRangeCommandLabel(stage, through, action, commandLabel)
			case through != "":
				runScope, err = converge.ApplyThroughScope(through)
				runCommandLabel = converge.ApplyThroughCommandLabel(through, action, commandLabel)
			default:
				runScope, err = converge.ApplyStageScope(stage)
				runCommandLabel = converge.ApplyStageCommandLabel(stage, action, commandLabel)
			}
			if err != nil {
				return failErr(2, err)
			}
		}
		if machinesScope != "" {
			stageProvided := stage != "" || through != ""
			var merr error
			runScope, merr = machineApplyRunScope(machinesScope, flags.clusterScope, stageProvided, runScope)
			if merr != nil {
				return merr
			}
			if !stageProvided {
				runCommandLabel = "machines " + action
			}
		}
		if reclaimDevices != "" && !converge.ScopeIncludesApplyPhase(runScope, converge.PhaseDeps) {
			return failErr(2, errors.New("--reclaim-devices wipes devices during the deps phase, which is not in this run's scope; re-run with a scope that includes it (--stage deps, --through base, or the full graph)"))
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
			return e
		}
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		printPlanStep(stdout, flags.output, runCommandLabel)
		sel, err := resolveScopeSelection(state, runScope.Name, flags.clusterScope, machinesScope)
		if err != nil {
			return failErr(1, err)
		}
		var secretScope *preflight.SecretScope
		if sel.Active {
			secretScope = &preflight.SecretScope{Machines: sel.WorkMachines, StorageClusters: sel.WorkStorageClusters}
		}
		if err := clusteraccess.ValidateScopedApplySharedServices(state, runScope.Name, flags.clusterScope); err != nil {
			return failErr(1, err)
		}
		if options.stageSelector {
			if err := validateKubeVirtClusterSelection(state, runScope, flags.clusterScope, clustersDir); err != nil {
				return failErr(1, err)
			}
		}
		if mode == workflow.ApplyModeOverride && converge.ScopeSkipsStorageDeviceGate(runScope) && converge.OverrideStorageDeviceGateApplies(sel.Active, sel.WorkStorageClusters, sel.RenderState) {
			return failErr(2, errors.New("--converge-drifted --stage base skips the deps-phase device-empty gate that must precede a Ceph wipe-and-rebuild; use --converge-drifted --through base (runs the gate then the rebuild) or the full graph"))
		}
		plan, err := prepareScopedApplyWorkflow(sel.RenderState, runScope, askBecomePass, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if converge.ScopeUsesAnsible(runScope) {
			if err := workflow.EnsureApplySupported(plan.State); err != nil {
				return failErr(1, err)
			}
		}
		applyTarget, tasks, limits, dryRunTasks, err := converge.PlanScopedApply(runScope, &plan, state, mode, sel.StorageWorkNames(), sel.Active, sel.MachineProvision, sel.WorkMachines, workflow.ConcurrencyLimits{}, ctx.RunsDir)
		if err != nil {
			return failErr(1, err)
		}
		converge.ApplyVerboseExtraVar(&plan, verbose)
		artifactServerTargets := installOnlyArtifactServerTargets(state)
		artifactReclaimPreview, _ := converge.ArtifactServerReclaimPreview(ctx.OwnershipDir, ctx.Name, clustersDir, artifactServerTargets)
		if skip, serr := converge.ArtifactServerProvisionSkipRecords(artifactServerTargets, clustersDir, mode); serr != nil {
			cliout.NewContinuation(stdout).Warning("artifact-server retention", serr.Error())
		} else {
			converge.ApplyArtifactServerSkipExtraVar(&plan, skip)
		}
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			var jsonReinstallDrift []string
			if mode == workflow.ApplyModeOverride {
				jsonReinstallDrift = workflow.OverrideReinstallInputDriftedClusters(clustersDir, ctx.Name, ctx.SecretsDir, plan.State, tasks)
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, action, plan.State, plan.Selected, runScope.ApplyPlaybook, plan.Limit, plan.ExtraVarPairs, runScope.ArtifactsBaseName, false, plan.AskBecomePass, plan.TargetsClusters, limits, dryRunTasks, nil, converge.BuildDryRunTransitions(tasks, ctx.RunsDir, mode, jsonReinstallDrift), workflow.AnsibleForksForLimit(plan.State, plan.Limit))
		}
		var destructiveOverride []string
		var substrateResetClusters []string
		var ocpReinstallDescriptors []string
		var ocpReinstallAcked []string
		if !dryRun {
			objects, err := converge.ApplyModePreflight(mode, tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, err)
			}
			if err := converge.CheckApplyRenameOrphan(state, objects, clustersDir); err != nil {
				return failErr(1, err)
			}
			releasedRecords, releaseErr := workflow.ConsumableSubstrateReleases(ctx.RunsDir, tasks)
			if releaseErr != nil {
				cliout.NewContinuation(stdout).Warning("substrate release", releaseErr.Error()+"; a destroyed cluster's rebuild authorization could not be read, so its reinstall may be refused — fix or remove the reported record and re-apply")
			}
			releasedClusters := workflow.SubstrateReleaseClusterNames(releasedRecords)
			if override {
				reinstalls := workflow.OverrideRebuildInstalledClusters(c.Context(), clustersDir, ctx.Name, ctx.SecretsDir, plan.State, tasks, nil)
				ocpReinstallDescriptors = workflow.ClusterReinstallDescriptors(reinstalls)
				ocpReinstallAcked = workflow.ClusterReinstallNames(reinstalls)
				if err := converge.CheckApplyOverrideDestroyProtection(plan.State, objects, ocpReinstallDescriptors); err != nil {
					return failErr(1, err)
				}
				destructiveOverride = workflow.OverrideDestructiveDriftedObjects(objects)
				destructiveOverride = append(destructiveOverride, ocpReinstallDescriptors...)
				_, substrateResetClusters = workflow.OverrideDestructiveMachineSubstrate(objects)
			}
			substrateResetClusters = workflow.UnionClusterNames(substrateResetClusters, releasedClusters)
			destructiveOverride = append(destructiveOverride, releasedBareMetalReinstallDescriptors(plan.State, releasedRecords)...)
			rebuiltHosts := workflow.UnionClusterNames(ocpReinstallAcked, substrateResetClusters)
			if err := checkKubeVirtTenantRebuildScope(state, clustersDir, flags.clusterScope, rebuiltHosts); err != nil {
				return failErr(1, err)
			}
			destructiveOverride = append(destructiveOverride, converge.KubeVirtTenantDestroyDescriptors(state, clustersDir, rebuiltHosts)...)
			if reclaimDevices != "" {
				ownedReclaim := converge.OwnedStorageClusters(objects)
				if err := converge.CheckReclaimDestroyProtection(plan.State, ownedReclaim, override); err != nil {
					return failErr(1, err)
				}
				if len(ownedReclaim) > 0 {
					if unmatched, declared := converge.UnmatchedReclaimDevices(plan.State, ownedReclaim, reclaimDevices); len(unmatched) > 0 {
						return failErr(2, reclaimUnmatchedError(unmatched, ownedReclaim, declared))
					}
				}
				destructiveOverride = append(destructiveOverride, reclaimDestructiveDescriptors(reclaimDevices, ownedReclaim)...)
			}
			if override && allowDestroy {
				destructiveOverride = append(destructiveOverride, filterReclaimDestructiveDescriptors(filterReclaimAuthorizedClusters(plan.State, objects))...)
			}
			if err := destructiveOverrideYesGuard(destructiveOverride, yes, allowDestroy); err != nil {
				return failErr(1, err)
			}
			converge.ApplyOCPRebuildAuthorizedClustersExtraVar(&plan, ocpReinstallAcked)
			emitApplyDataLossWarningsAndVars(stdout, mode, objects, tasks, &plan, reclaimDevices, releasedRecords, clustersDir, ocpReinstallDescriptors, allowDestroy)
			noteIneffectiveAllowDestroy(stdout, allowDestroy, false, destructiveOverride)
			if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			hostTrustScope := workflow.ApplyTaskConnectedMachines(tasks)
			if trustOnFirstUse && !yes && flags.output == outputText {
				if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, plan.State, defaultHostTrustDeps, hostTrustScope); err != nil {
					return failErr(1, err)
				}
			}
			if err := runApplyHostCheck(stdout, stderr, plan.State, plan.Selected, ctx.Name, ctx.SecretsDir, clustersDir, ctx.RunsDir, hostTrustScope, secretScope); err != nil {
				return err
			}
		}
		if options.stageSelector {
			printStageScopeNotices(stdout, runScope)
		}
		printApplySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		if !dryRun {
			warnDestructiveApply(stdout, destructiveOverride)
			printArtifactServerReclaimNotice(stdout, artifactReclaimPreview)
		}
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, destructiveApplyConfirmPrompt(destructiveOverride, allowDestroy)) {
				return failErr(1, errors.New("apply aborted"))
			}
		}
		if !dryRun {
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			for _, problem := range workflow.RemoveProvisioningPlaybookRecordsForClusters(ctx.RunsDir, plan.State, tasks, substrateResetClusters) {
				cliout.NewContinuation(stdout).Warning("stale records", problem.Error()+"; the playbook may be skipped as unchanged although its machines are rebuilt")
			}
		}
		become, reporter, becomeCleanup, err := prepareMutatingRunCredential(stdin, stdout, stderr, plan, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		defer becomeCleanup()
		usesAnsible := converge.ScopeUsesAnsible(runScope)
		var bundleResult bundle.AnsibleBundleResult
		if usesAnsible {
			bundleResult, err = prepareWorkflowBundle(true)
			if err != nil {
				return failErr(1, err)
			}
		}
		runOpts := converge.BuildApplyRunOptions(ctx, clustersDir, flags.executable, runScope, plan, false, become.PasswordFile, dryRun, runCommandLabel, mode, false)
		runOpts.OverrideAckedReinstalls = ocpReinstallAcked
		runOpts.SelectedMachines = sel.MachineScopeNames()
		if dryRun {
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight "+runScope.Name+" to validate secrets, tools, and remote readiness")
			if reclaimDevices != "" {
				cliout.NewContinuation(stdout).Warning("reclaim", "a real run would WIPE device(s) "+reclaimDevices+" on any selected Bootwright-owned Ceph cluster before apply, on hosts whose OSD marker does not already record the device — irreversible data loss, gated by the data-loss acknowledgement (--confirm-data-loss or interactive confirm)")
			}
			reporter.DryRunTasks(runCommandLabel, workflow.TaskLedgerEntries(dryRunTasks), limits)
			var reinstallDrift []string
			if mode == workflow.ApplyModeOverride {
				reinstallDrift = workflow.OverrideReinstallInputDriftedClusters(clustersDir, ctx.Name, ctx.SecretsDir, plan.State, tasks)
			}
			printApplyTransitionLedger(stdout, tasks, ctx.RunsDir, mode, reinstallDrift)
			printApplyAvailabilityCaveat(stdout, mode, clustersDir, tasks)
			printApplyGateForecast(stdout, state, plan.State, tasks, ctx.RunsDir, clustersDir, mode, reclaimDevices, flags.clusterScope, reinstallDrift)
			printArtifactServerReclaimNotice(stdout, artifactReclaimPreview)
			noteIneffectiveAllowDestroy(stdout, allowDestroy, true, nil)
			printExtensionDryRun(stdout, dryRunTasks)
			printProvisioningPlaybookDryRun(stdout, dryRunTasks)
			result, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
			if err != nil {
				return failErr(1, err)
			}
			printRenderResult(stdout, result)
			if usesAnsible {
				printBundlePath(stdout, bundleResult.Dir)
			}
			return nil
		}
		renderResult, bundleResult, ledger, err := converge.ExecuteApply(c.Context(), stdout, stderr, ctx, clustersDir, runOpts, applyTarget, flags.clusterScope, plan, tasks, limits, usesAnsible, bundleResult, bundleVersionMarker(), reporter, newApplyReporter(stdout, stderr, ctx.Name, ctx.RunsDir, clustersDir, buildClusterDisplays(state), false))
		if err != nil {
			if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
				return silentExit(1)
			}
			return failErr(1, err)
		}
		printRenderResult(stdout, renderResult)
		if usesAnsible {
			printBundlePath(stdout, bundleResult.Dir)
		}
		if usesAnsible {
			if rerr := converge.ReclaimInstallOnlyArtifactServers(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundleResult.Dir, become.PasswordFile, state, artifactServerTargets, reporter); rerr != nil {
				cliout.NewContinuation(stdout).Warning("artifact-server reclaim", rerr.Error())
			}
		}
		if plan.TargetsClusters {
			printClusterAccess(stdout, plan.State, renderResult, ledger, ctx.Name, clustersDir)
		}
		return nil
	}
	return cmd
}
