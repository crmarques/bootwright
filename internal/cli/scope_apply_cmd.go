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

func newScopeApplyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeApplyOptions) *cobra.Command {
	usesAnsible := converge.ScopeUsesAnsible(scope)
	var (
		flags           scopeCommonFlags
		dryRun          = options.defaultPlan
		askBecomePass   bool
		yes             bool
		trustOnFirstUse bool
		verbose         bool
		modeFlag        string
		authorizeFlag   []string
		stage           string
		through         string
		reclaimDevices  string
		machinesScope   string
	)
	labels := resolveScopeApplyLabels(scope, options)
	commandLabel, action := labels.commandLabel, labels.action
	cmd := &cobra.Command{
		Use:     labels.use,
		Short:   labels.short,
		Long:    options.long,
		Args:    cobra.NoArgs,
		Example: labels.example,
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
	addModeFlag(cmd, &modeFlag, action)
	addAuthorizeFlag(cmd, &authorizeFlag, authorizeVerbApply)
	if usesAnsible {
		cmd.Flags().StringVar(&reclaimDevices, "reclaim-devices", "", "comma-separated block-device paths to WIPE in-band before a managed-Ceph apply, or the single word "+converge.ReclaimDevicesAll+" to select every declared OSD device of the selected owned cluster(s) (recover owned OSD disks whose on-node marker was lost by a managed-OS reinstall); only wipes a selected device that is a declared OSD device of a Bootwright-owned cluster, is not mounted or a system disk, and is on a host whose OSD marker does not already record it — irreversible data loss")
		registerFlagCompletion(cmd, "reclaim-devices", []string{converge.ReclaimDevicesAll})
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
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to "+action+" (default: all)")
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
		for _, name := range []string{"reclaim-devices", "ask-become-pass", "verbose"} {
			_ = cmd.Flags().MarkHidden(name)
		}
	}
	cmd.RunE = func(c *cobra.Command, _ []string) (returnErr error) {
		var runLease *workflow.CommandRunLease
		defer func() {
			returnErr = closeMutatingRunLease(returnErr, runLease)
		}()
		runContext := c.Context()
		mode, auth, err := resolveScopeApplyIntent(flags.output, options.defaultPlan, dryRun, modeFlag, authorizeFlag)
		if err != nil {
			return err
		}
		override := mode == workflow.ApplyModeRebuild
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
		selection := runSelection{stage: stage, through: through, clusters: flags.clusterScope, machines: machinesScope}
		if err := converge.ValidateReclaimDevicesFlag(reclaimDevices); err != nil {
			return failErr(2, err)
		}
		if reclaimDevices != "" && !converge.ScopeIncludesApplyPhase(runScope, converge.PhaseDeps) {
			return failErr(2, errors.New("--reclaim-devices wipes devices during the deps phase, which is not in this run's scope; re-run with a scope that includes it (--stage deps, --through base, or the full graph)"))
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		invocation, err := newResolvedInvocation(invocationApply, ctx.Name, invocationFlags{
			mode:            mode,
			selection:       selection,
			reclaimDevices:  reclaimDevices,
			authorizations:  auth.all(),
			dryRun:          dryRun,
			output:          flags.output,
			yes:             yes,
			askBecomePass:   askBecomePass,
			trustOnFirstUse: trustOnFirstUse,
			verbose:         verbose,
		})
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
			return e
		}
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		if !dryRun {
			if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
				return failErr(1, mutatingRunLeaseRefusal(err, invocation))
			}
			runLease, err = workflow.AcquireCommandRunLease(c.Context(), ctx.RunsDir, "apply")
			if err != nil {
				return failErr(1, mutatingRunLeaseRefusal(err, invocation))
			}
			runContext = runLease.Context()
		}
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
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
		if mode == workflow.ApplyModeRebuild && converge.ScopeSkipsStorageDeviceGate(runScope) && converge.OverrideStorageDeviceGateApplies(sel.Active, sel.WorkStorageClusters, sel.RenderState) {
			return failErr(2, errors.New("--mode rebuild --stage base skips the deps-phase device-empty gate that must precede a Ceph wipe-and-rebuild; use --mode rebuild --through base (runs the gate then the rebuild) or the full graph"))
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
		applyTarget, tasks, limits, dryRunTasks, err := converge.PlanScopedApply(runContext, runScope, &plan, state, mode, sel.StorageWorkNames(), sel.Active, sel.MachineProvision, sel.WorkMachines, workflow.ConcurrencyLimits{}, ctx.RunsDir, ctx.Name, ctx.SecretsDir)
		if err != nil {
			return failErr(1, err)
		}
		converge.ApplyVerboseExtraVar(&plan, verbose)
		artifactServerTargets := installOnlyArtifactServerTargets(state)
		artifactReclaimPreview, _ := converge.ArtifactServerReclaimPreview(ctx.OwnershipDir, ctx.Name, clustersDir, artifactServerTargets)
		ownershipRecords, ownershipSkipped, err := applyOwnershipRecords(ctx, dryRun, &invocation)
		if err != nil {
			return failErr(1, err)
		}
		provisionedStorageTenants := converge.ProvisionedStorageTenants(ownershipRecords)
		if skip, serr := converge.ArtifactServerProvisionSkipRecords(artifactServerTargets, clustersDir, mode); serr != nil {
			cliout.NewContinuation(c.ErrOrStderr()).Warning("artifact-server retention", serr.Error())
		} else {
			converge.ApplyArtifactServerSkipExtraVar(&plan, skip)
		}
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			jsonReinstallDrift := applyJSONReinstallDrift(mode, clustersDir, ctx.RunsDir, ctx.Name, ctx.SecretsDir, plan.State, tasks)
			jsonRequiredAuth := applyRequiredAuthorizations(auth, mode, state, plan.State, tasks, ctx.RunsDir, clustersDir, jsonReinstallDrift, reclaimDevices, provisionedStorageTenants)
			jsonRefusals := applyGateForecastRefusals(state, plan.State, tasks, ctx.RunsDir, clustersDir, mode, auth.has(authorizeDataLoss), auth.has(authorizeUnownedDevices), reclaimDevices, sel, jsonReinstallDrift, ctx.OwnershipDir, ownershipRecords, ownershipSkipped, &invocation)
			return runScopeDryRunJSONAuthorized(c, stdout, cf, flags, runScope, action, plan.State, plan.Selected, runScope.ApplyPlaybook, plan.Limit, plan.ExtraVarPairs, runScope.ArtifactsBaseName, plan.AskBecomePass, plan.TargetsClusters, limits, dryRunTasks, nil, converge.BuildDryRunTransitions(tasks, ctx.RunsDir, mode, jsonReinstallDrift), workflow.AnsibleForksForLimit(plan.State, plan.Limit), jsonRequiredAuth, dryRunDisclosure{refusals: jsonRefusals})
		}
		var destructiveOverride []string
		var substrateResetClusters []string
		var ocpReinstallDescriptors []string
		var ocpReinstallAcked []string
		forecastReleasedReinstallDataLoss(stdout, dryRun, ctx.RunsDir, plan.State, tasks)
		if !dryRun {
			objects, err := converge.ApplyModePreflight(mode, tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, applyModePreflightRefusal(err, invocation))
			}
			if err := converge.CheckApplyRenameOrphan(state, objects, clustersDir, ownershipRecords); err != nil {
				return failErr(1, applyRenameOrphanRefusal(err, invocation))
			}
			releasedRecords, releaseErr := workflow.ConsumableSubstrateReleases(ctx.RunsDir, tasks)
			if releaseErr != nil {
				cliout.NewContinuation(stdout).Warning("substrate release", releaseErr.Error()+"; a destroyed cluster's rebuild authorization could not be read, so its reinstall may be refused — fix or remove the reported record and re-apply")
			}
			if err := installedContainerClusterMachineReleaseRefusal(clustersDir, releasedRecords, &invocation); err != nil {
				return failErr(1, err)
			}
			releasedClusters := workflow.SubstrateReleaseClusterNames(releasedRecords)
			var ownedReclaim []string
			if override {
				var rerr error
				if ocpReinstallDescriptors, ocpReinstallAcked, rerr = overrideReinstallPlan(runContext, clustersDir, ctx.RunsDir, ctx.Name, ctx.SecretsDir, plan.State, tasks, applyClusterAvailabilityChecker); rerr != nil {
					return failErr(1, applyInstallRemedialError(rerr, invocation))
				}
				if err := converge.CheckApplyOverrideDestroyProtection(plan.State, objects, ocpReinstallDescriptors); err != nil {
					return failErr(1, err)
				}
				_, substrateResetClusters = workflow.OverrideDestructiveMachineSubstrate(objects)
			}
			substrateResetClusters = workflow.UnionClusterNames(substrateResetClusters, releasedClusters)
			rebuiltHosts := workflow.UnionClusterNames(ocpReinstallAcked, substrateResetClusters)
			if err := checkKubeVirtTenantRebuildScope(state, clustersDir, sel, rebuiltHosts, provisionedStorageTenants, &invocation); err != nil {
				return failErr(1, err)
			}
			if reclaimDevices != "" {
				var rerr error
				if reclaimDevices, ownedReclaim, rerr = resolveApplyReclaimDevices(stdout, &plan, auth, objects, reclaimDevices, invocation); rerr != nil {
					return rerr
				}
			}
			applyForeignCephadmDaemons(stdout, &plan, auth, tasks)
			allowDestroy := auth.has(authorizeDataLoss)
			destructiveOverride = applyRunDestructiveDescriptors(mode, objects, state, plan.State, clustersDir, ocpReinstallDescriptors, releasedRecords, rebuiltHosts, reclaimDevices, ownedReclaim, allowDestroy, provisionedStorageTenants)
			if len(destructiveOverride) > 0 {
				auth.note(authorizeDataLoss)
			}
			if err := destructiveOverrideYesGuard(destructiveOverride, yes, allowDestroy, invocation); err != nil {
				return failErr(1, err)
			}
			converge.ApplyOCPRebuildAuthorizedClustersExtraVar(&plan, ocpReinstallAcked)
			if emitApplyDataLossWarningsAndVars(stdout, mode, objects, tasks, &plan, reclaimDevices, ownedReclaim, releasedRecords, clustersDir, ocpReinstallDescriptors, allowDestroy) {
				auth.note(authorizeDataLoss)
			}
			warnUnusedAuthorizations(stdout, auth, false)
			hostTrustScope := workflow.ApplyTaskConnectedMachines(tasks)
			if trustOnFirstUse && !yes && flags.output == outputText {
				if err := offerTrustOnFirstUse(runContext, stdin, stdout, ctx.BaseDir, plan.State, defaultHostTrustDeps, hostTrustScope); err != nil {
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
			warnDestructiveApply(stdout, destructiveOverride, selection)
			printArtifactServerReclaimNotice(stdout, artifactReclaimPreview)
		}
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, destructiveApplyConfirmPrompt(destructiveOverride, auth.has(authorizeDataLoss))) {
				return failErr(1, errors.New("apply aborted"))
			}
		}
		if !dryRun {
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			for _, problem := range workflow.RemovePlaybookRecordsForClusters(ctx.RunsDir, plan.State, tasks, substrateResetClusters) {
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
		if !dryRun && usesAnsible {
			if err := appendMutatingInvocationExtraVars(&plan, invocation, reclaimDevices); err != nil {
				return failErr(1, err)
			}
		}
		runOpts := converge.BuildApplyRunOptions(ctx, clustersDir, flags.executable, runScope, plan, false, become.PasswordFile, dryRun, runCommandLabel, mode, false)
		runOpts.RunLease = runLease
		runOpts.OverrideAckedReinstalls = ocpReinstallAcked
		runOpts.SelectedMachines = sel.MachineScopeNames()
		runOpts.ClusterAvailabilityChecker = applyClusterAvailabilityChecker
		if dryRun {
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight "+runScope.Name+" to validate secrets, tools, and remote readiness")
			if reclaimDevices != "" {
				cliout.NewContinuation(stdout).Warning("reclaim", "a real run would WIPE "+describeReclaimSelection(reclaimDevices)+" on any selected Bootwright-owned Ceph cluster before apply, on hosts whose OSD marker does not already record the device — irreversible data loss, gated by --authorize "+authorizeDataLoss+" or the interactive data-loss confirm")
			}
			planEntries := workflow.TaskLedgerEntries(dryRunTasks)
			reporter.DryRunTasks(runCommandLabel, planEntries, limits, applyPlanGroups(planEntries, buildClusterDisplays(state)))
			var reinstallDrift []string
			if mode == workflow.ApplyModeRebuild {
				reinstallDrift = workflow.OverrideReinstallInputDriftedClusters(clustersDir, ctx.RunsDir, ctx.Name, ctx.SecretsDir, plan.State, tasks)
			}
			printApplyTransitionLedger(stdout, tasks, ctx.RunsDir, mode, reinstallDrift)
			printApplyAvailabilityCaveat(stdout, mode, clustersDir, tasks)
			printApplyGateForecast(stdout, state, plan.State, tasks, ctx.RunsDir, clustersDir, mode, auth.has(authorizeDataLoss), auth.has(authorizeUnownedDevices), reclaimDevices, sel, reinstallDrift, ctx.OwnershipDir, ownershipRecords, ownershipSkipped, &invocation)
			printArtifactServerReclaimNotice(stdout, artifactReclaimPreview)
			printRequiredAuthorizations(stdout, applyRequiredAuthorizations(auth, mode, state, plan.State, tasks, ctx.RunsDir, clustersDir, reinstallDrift, reclaimDevices, provisionedStorageTenants))
			warnUnusedAuthorizations(stdout, auth, true)
			printExtensionDryRun(stdout, dryRunTasks)
			printPlaybookDryRun(stdout, dryRunTasks)
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
		renderResult, bundleResult, ledger, err := converge.ExecuteApply(runContext, stdout, stderr, ctx, clustersDir, runOpts, applyTarget, flags.clusterScope, plan, tasks, limits, usesAnsible, bundleResult, bundleVersionMarker(), reporter, newApplyReporter(stdout, stderr, ctx.Name, ctx.RunsDir, clustersDir, buildClusterDisplays(state), false))
		if err != nil {
			if hasApplyInstallRemedy(err) {
				return failErr(1, applyInstallRemedialError(err, invocation))
			}
			if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
				return silentExit(1)
			}
			return failErr(1, applyInstallRemedialError(err, invocation))
		}
		printRenderResult(stdout, renderResult)
		if usesAnsible {
			printBundlePath(stdout, bundleResult.Dir)
		}
		if usesAnsible {
			if rerr := converge.ReclaimInstallOnlyArtifactServers(runContext, stdout, stderr, ctx, clustersDir, flags.executable, bundleResult.Dir, become.PasswordFile, state, artifactServerTargets, reporter, runLease); rerr != nil {
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
