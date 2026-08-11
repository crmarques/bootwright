package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newScopeDestroyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeDestroyOptions) *cobra.Command {
	var (
		flags                                             scopeCommonFlags
		dryRun, askBecomePass, yes, purgeHistory, verbose bool
		authorizeFlag                                     []string
		cephRecovery, stage, machinesScope                string
	)
	labels := resolveScopeDestroyLabels(scope, options)
	commandLabel := labels.commandLabel
	cmd := newScopeDestroyCommand(labels, options)
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, flagDryRunUsage)
	addAskBecomePassFlag(cmd, &askBecomePass)
	addYesFlag(cmd, &yes, "destroy")
	addAuthorizeFlag(cmd, &authorizeFlag, authorizeVerbDestroy)
	cmd.Flags().StringVar(&cephRecovery, "recover-ceph-ownership", "", "recover missing Ceph controller and host ownership evidence before destroy, as comma-separated <StorageCluster>=<fsid> entries; requires a matching selected managed cluster and an exact /etc/ceph/ceph.conf fsid match, and refuses contradictory controller records. Does not bypass OSD-device safety checks and authorizes no named risk of its own")
	cmd.Flags().BoolVar(&purgeHistory, "purge-history", false, "once a cluster's or machine's teardown succeeds, also delete its retained history: its whole state tree under this context's clusters/<name>/ (installer working directory, install/connection records, kubeconfig, captured cluster secrets) and its per-run task and flow logs under runs/. Scoped identically to --clusters/--machines; a fully successful unscoped destroy instead sweeps the context's whole run history — every archived run, earlier destroy attempts, and crashed runs that never archived a ledger — keeping only this destroy run's own record. Never touches a component outside that scope, a partially-destroyed cluster kept for retry, an unrelated run's shared ledger, or the provider state of a machine layer this run leaves standing (--stage clusters). Does not remove the destroy-authorization substrate-release record or the context's ownership/input-history stores")
	addVerboseFlag(cmd, &verbose)
	registerScopeDestroySelectionFlags(cmd, &flags, scope, options.stageSelector, &stage, &machinesScope)
	cmd.RunE = func(c *cobra.Command, _ []string) (returnErr error) {
		var runLease, sharedServiceLease *workflow.CommandRunLease
		defer func() {
			returnErr = closeMutatingRunLease(returnErr, sharedServiceLease)
			returnErr = closeMutatingRunLease(returnErr, runLease)
		}()
		runContext := c.Context()
		auth, err := resolveScopeDestroyIntent(flags.output, dryRun, authorizeFlag)
		if err != nil {
			return err
		}
		runScope, runCommandLabel, selection, err := resolveScopeDestroyRun(scope, commandLabel, options.stageSelector, stage, flags.clusterScope, machinesScope)
		if err != nil {
			return err
		}
		fullDestroy := converge.DestroyIsFullScope(runScope)
		forceUnowned, forceUnownedNetworks := destroyUnownedAuthorizations(runScope, auth)
		confirmedCephFSIDs, err := resolveDestroyCephRecovery(runScope, cephRecovery)
		if err != nil {
			return err
		}
		ctx, err := cf.resolveLocalOnly()
		if err != nil {
			return failErr(1, err)
		}
		invocation, err := newResolvedInvocation(invocationDestroy, ctx.Name, invocationFlags{
			selection:            selection,
			recoverCephOwnership: cephRecovery,
			purgeHistory:         purgeHistory,
			authorizations:       auth.all(),
			dryRun:               dryRun,
			output:               flags.output,
			yes:                  yes,
			askBecomePass:        askBecomePass,
			verbose:              verbose,
		})
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		if flags.output != outputJSON {
			printUnscopedFullDestroyDisclosure(stdout, fullDestroy, selection, yes)
		}
		if !dryRun {
			if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
				return failErr(1, mutatingRunLeaseRefusal(err, invocation))
			}
			runLease, err = workflow.AcquireCommandRunLease(c.Context(), ctx.RunsDir, "destroy")
			if err != nil {
				return failErr(1, mutatingRunLeaseRefusal(err, invocation))
			}
			runContext = runLease.Context()
		}
		var state v1alpha1.State
		state, stateSkipped, err := loadDesiredStateTolerant(cf)
		if err != nil {
			return failErr(1, err)
		}
		inputSkipped := stateSkipped
		staleInputReached := len(inputSkipped) > 0
		if staleInputReached && !auth.allows(authorizeStaleInput) {
			return failErr(1, staleInputRefusal(ctx.Name, inputSkipped, invocation))
		}
		artifactServerOnly := converge.IsInfraArtifactServerDestroyScope(runScope, flags.clusterScope)
		if purgeHistory && artifactServerOnly {
			return failErr(2, errors.New("--purge-history has no per-component history to remove for the artifact-server service; drop --purge-history"))
		}
		var sel clusteraccess.Selection
		if !artifactServerOnly {
			sel, err = resolveScopeSelection(state, runScope.Name, flags.clusterScope, machinesScope)
			if err != nil {
				return failErr(1, err)
			}
		}
		allOwnershipRecords, ownershipSkipped, err := ownership.LoadResourcesWithWarnings(ctx.OwnershipDir)
		if err != nil {
			return failErr(1, err)
		}
		ownershipRecords := ownership.FilterByContext(allOwnershipRecords, ctx.Name)
		unreadableRecordsReached := len(ownershipSkipped) > 0
		if unreadableRecordsReached && !dryRun && !auth.allows(authorizeUnreadableRecords) {
			return failErr(1, unreadableOwnershipRefusal(ctx, ownershipSkipped, invocation))
		}
		if err := converge.ValidateDestroyCephOwnershipRecovery(sel.RenderState, sel.StorageWorkNames(), ctx.OwnershipDir, ctx.Name, allOwnershipRecords, confirmedCephFSIDs); err != nil {
			return failErr(1, destroyCephOwnershipRecoveryRefusal(err, ctx.OwnershipDir, invocation))
		}
		if err := destroyScopeConflictGates(state, sel, runScope, fullDestroy, ctx.RunsDir, clustersDir, ownershipRecords, invocation); err != nil {
			return failErr(1, err)
		}
		sharedInfraReached, storageConsumerOverrideNotice, consumerErr := destroyStorageConsumerGate(auth, state, sel, runScope, dryRun, invocation)
		if consumerErr != nil {
			return failErr(1, consumerErr)
		}
		installedClusterNodeReached := false
		if sel.MachineSelection {
			if err := machineDestroyInstalledClusterGuard(clustersDir, sel.ContainerRoots, sel.StorageRoots, ownershipRecords, invocation); err != nil {
				installedClusterNodeReached = true
				if !auth.allows(authorizeInstalledClusterNode) && !dryRun {
					return failErr(1, err)
				}
			}
		}
		playbook := runScope.DestroyPlaybook
		artifactsBaseName := runScope.ArtifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		evidenceDegraded := staleInputReached || unreadableRecordsReached
		sharedMutation, err := prepareDestroySharedServiceMutation(runContext, ctx.Name, state, sel, runScope, artifactServerOnly, dryRun, evidenceDegraded, ownershipRecords, auth, invocation)
		sharedServiceLease, runContext = sharedMutation.lease, sharedMutation.runContext
		sharedInfraReached = sharedInfraReached || sharedMutation.reached
		componentDecision := sharedMutation.decision
		if err != nil {
			return failErr(1, err)
		}
		var sharedServiceForecast error
		if dryRun && sharedMutation.refusal != nil {
			sharedServiceForecast = applyInstallRemedialError(sharedMutation.refusal, invocation)
		}
		var plan converge.WorkflowPlan
		if artifactServerOnly {
			plan = converge.PrepareInfraArtifactServerDestroyWorkflow(state, askBecomePass, dryRun, ownershipRecords)
			playbook = converge.InfraDestroyArtifactServerPlaybook
			artifactsBaseName = converge.InfraDestroyArtifactServerArtifactsBaseName
			workflowLabel = "infra destroy artifact-server"
		} else {
			plan, err = prepareScopedWorkflow(sel.RenderState, runScope, askBecomePass, dryRun, ownershipRecords)
			if err != nil {
				return failErr(1, err)
			}
			plan.StorageWorkNames = sel.StorageWorkNames()
			plan.SelectedMachines = sel.MachineScopeNames()
		}
		infraScope := !artifactServerOnly && (runScope.Name == "infra" || fullDestroy)
		var resolvedClusterRoots []string
		if infraScope && sel.Active {
			resolvedClusterRoots = sel.AllRoots
		}
		skipUnreachable := auth.has(authorizeUnreachableNodes)
		if skipUnreachable && !plan.NoRemoteWork {
			auth.note(authorizeUnreachableNodes)
		}
		postDestroyRetry, err := resolvedPostDestroyRetry(invocation, skipUnreachable)
		if err != nil {
			return failErr(1, err)
		}
		converge.ApplyDestroyScopeExtraVars(&plan, infraScope, flags.clusterScope, resolvedClusterRoots, sel.MachineScopeNames(), forceUnowned, forceUnownedNetworks, skipUnreachable, destroyAuthorizesUnownedDevices(auth, runScope))
		converge.ApplyDestroyEvidenceDegradedExtraVar(&plan, staleInputReached || unreadableRecordsReached)
		if err := converge.ApplyDestroyCephOwnershipRecoveryExtraVar(&plan, confirmedCephFSIDs); err != nil {
			return failErr(1, err)
		}
		converge.ApplyVerboseExtraVar(&plan, verbose)
		safetyScope := workflow.DestroySafetyScope{}
		if !artifactServerOnly {
			safetyScope.TearsMachines = converge.ScopeTearsMachineLayer(runScope)
			safetyScope.TearsClusters = converge.ScopeTearsClusterLayer(runScope)
		}
		authorizedProtected := auth.has(authorizeProtected)
		destroySafety := workflow.EvaluateDestroySafety(plan.State, authorizedProtected, plan.StorageWorkNames, safetyScope)
		if len(destroySafety.Reasons) > 0 {
			auth.note(authorizeProtected)
		}
		storageScopeNames := converge.DestroyStorageScopeNames(plan.State, plan.StorageWorkNames)
		dataLoss := workflow.EvaluateDestroyDataLoss(plan.State, storageScopeNames, safetyScope)
		dataLossReached := dataLoss.Planned() && !plan.NoRemoteWork
		requiredAuth := destroyRequiredAuthorizations(auth, destroyGateForecast{
			runScope:            runScope,
			noRemoteWork:        plan.NoRemoteWork,
			staleInput:          staleInputReached,
			unreadableRecords:   unreadableRecordsReached,
			sharedInfra:         sharedInfraReached,
			installedNode:       installedClusterNodeReached,
			protected:           len(destroySafety.Reasons) > 0,
			protectedReason:     destroySafety.Summary(),
			dataLoss:            dataLossReached,
			dataLossReason:      dataLoss.Consequence(),
			unreadableRecordDir: ctx.OwnershipDir,
		})
		if flags.output == outputJSON {
			disclosure := destroyDryRunDisclosure(destroySafety, inputSkipped, ownershipSkipped, componentDecision, sharedServiceForecast, auth, purgeHistory && !plan.NoRemoteWork)
			return runDestroyDryRunJSON(c, stdout, cf, flags, runScope, plan, playbook, artifactsBaseName, artifactServerOnly, converge.DestroyDryRunSafetyReport(destroySafety, authorizedProtected), requiredAuth, disclosure)
		}
		if !dryRun && destroySafety.RequiresAuthorization {
			command, retryErr := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeProtected}})
			if retryErr != nil {
				return failErr(1, retryErr)
			}
			return failErr(1, fmt.Errorf("%s; this requires --authorize %s; re-run `%s` to destroy it anyway", destroySafety.Summary(), authorizeProtected, command.String()))
		}
		printDestroySafety(stdout, destroySafety, authorizedProtected, dryRun)
		if storageConsumerOverrideNotice != "" {
			cliout.NewContinuation(stdout).Warning("storage consumers", storageConsumerOverrideNotice)
		}
		if len(confirmedCephFSIDs) > 0 {
			cliout.NewContinuation(stdout).Warning("ceph ownership recovery", "before teardown, Bootwright will reconstruct the selected cluster's controller record and host marker only where /etc/ceph/ceph.conf on the declared seed exactly matches the supplied fsid; contradictory controller ownership evidence is refused")
		}
		printSkippedInputDocuments(stdout, inputSkipped, auth.has(authorizeStaleInput))
		printSkippedOwnershipRecords(stdout, ownershipSkipped)
		if artifactServerOnly {
			printDestroyArtifactServerPreview(stdout, plan.State)
		} else {
			printDestroyPreview(stdout, runScope, clustersDir, plan.State, plan.StorageWorkNames, sel.MachineScopeNames())
			printDestroyOrphans(stdout, workflow.OwnershipOrphans(state, ownershipRecords))
		}
		printInfraComponentDestroyBlocks(stdout, componentDecision, auth.has(authorizeSharedInfra))
		if sharedServiceForecast != nil {
			printApplyGateRefusals(stdout, []string{sharedServiceForecast.Error()})
		}
		printDestroySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		dataLossPlanned := dataLossReached && !dryRun
		allowDestroy := auth.has(authorizeDataLoss)
		if dataLossPlanned {
			auth.note(authorizeDataLoss)
			cliout.NewContinuation(stdout).Warning("data loss", dataLoss.Warning())
		}
		if purgeHistory && !plan.NoRemoteWork && (dryRun || !yes) {
			cliout.NewContinuation(stdout).Warning("purge history", destroyPurgeHistoryNotice(dryRun))
		}
		if purgeHistory && skipUnreachable && !plan.NoRemoteWork {
			cliout.NewContinuation(stdout).Warning("purge history", "--authorize "+authorizeUnreachableNodes+" proves no per-node completion outside a managed storage cluster, so this run keeps the history and state tree of every container cluster and machine layer it touches — that history is what a retry and a diagnosis read. Only a storage cluster whose teardown report names no skipped node is purged. Once every node answers, re-run the same teardown without that authorization: `"+postDestroyRetry.String()+"`")
		}
		if dryRun {
			printRequiredAuthorizations(stdout, requiredAuth)
		}
		warnUnusedAuthorizations(stdout, auth, dryRun)
		if dataLossPlanned {
			if err := destroyDataLossYesGuard(dataLoss, yes, allowDestroy, invocation); err != nil {
				return failErr(1, err)
			}
		}
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, destroyConfirmPrompt(dataLoss.Planned() && !allowDestroy)) {
				return failErr(1, errors.New("destroy aborted"))
			}
		}
		if !dryRun {
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
		}
		if !dryRun && !plan.NoRemoteWork {
			if err := converge.SnapshotMutatingRunInput(workflow.LastDestroyInputSnapshotDir(ctx.RunsDir), ctx); err != nil {
				return failErr(1, err)
			}
		}
		become, reporter, becomeCleanup, err := prepareMutatingRunCredential(stdin, stdout, stderr, plan, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		defer becomeCleanup()
		if !dryRun && !plan.NoRemoteWork {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(dryRun || plan.NoRemoteWork)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.NoRemoteWork {
			reporter.BundleReady(bundle)
		}
		if !dryRun && !plan.NoRemoteWork {
			if err := appendMutatingInvocationExtraVars(&plan, invocation, ""); err != nil {
				return failErr(1, err)
			}
			if err := appendHostSharedServiceManifestExtraVar(&plan, sharedMutation.manifest); err != nil {
				return failErr(1, err)
			}
		}
		useGraph := !dryRun && !plan.NoRemoteWork && !artifactServerOnly
		if err := requireSharedServiceMutationLease(sharedServiceLease, "before destroy execution"); err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.NoRemoteWork && len(sharedMutation.manifest) > 0 {
			defer func() {
				returnErr = finishHostSharedServiceOperations(returnErr, func() error {
					if err := requireSharedServiceMutationLease(sharedServiceLease, "before host-wide operation finalization"); err != nil {
						return err
					}
					if err := converge.FinalizeHostSharedServiceOperations(runContext, stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, become.PasswordFile, plan, sharedMutation.manifest, ownershipRecords, reporter, runLease, invocation.args()); err != nil {
						return hostSharedServiceFinalizationRefusal(err, invocation)
					}
					return nil
				})
			}()
		}
		var renderResult render.Result
		switch {
		case dryRun && !artifactServerOnly:
			tasks, terr := workflow.PlanDestroyTasks(runScope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
			if terr != nil {
				return failErr(1, terr)
			}
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight to validate secrets, tools, and remote readiness")
			planEntries := workflow.TaskLedgerEntries(tasks)
			reporter.DryRunTasks(runCommandLabel, planEntries, workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks), destroyPlanGroups(planEntries))
			result, rerr := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
			if rerr != nil {
				return failErr(1, rerr)
			}
			renderResult = result
		case useGraph:
			dr := newDestroyReporter(stdout, stderr, ctx.RunsDir, false)
			result, ledger, runLogPath, gerr := converge.ExecuteDestroyGraph(runContext, stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, runScope.Name, flags.clusterScope, plan, false, become.PasswordFile, false, workflowLabel, dr, runLease, invocation.args())
			destroyOutcome, skippedErr := destroyGraphCompletion(ledger, invocation)
			partial, partialErr := recordStorageDestroyCompletion(
				ctx.OwnershipDir, ctx.Name, runLogPath, plan.State, ledger,
				storageScopeNames, skipUnreachable, postDestroyRetry,
			)
			resetPartial := partial.Clusters
			if partialErr != nil {
				resetPartial = storageScopeNames
			}
			if err := runLease.RequireOwned(); err != nil {
				return failErr(1, err)
			}
			if err := requireSharedServiceMutationLease(sharedServiceLease, "before post-destroy evidence cleanup"); err != nil {
				return failErr(1, err)
			}
			if gerr != nil {
				printPartialStorageDestroyWarning(stdout, partial, partialErr, postDestroyRetry)
				_ = printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, destroyOutcome, ledger.RunID, purgeHistory, skipUnreachable, postDestroyRetry)
				if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
					return silentExit(1)
				}
				return failErr(1, gerr)
			}
			resetErr := printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, destroyOutcome, ledger.RunID, purgeHistory, skipUnreachable, postDestroyRetry)
			printPartialStorageDestroyWarning(stdout, partial, partialErr, postDestroyRetry)
			if resetErr != nil {
				return failErr(1, resetErr)
			}
			if partialErr != nil {
				return failErr(1, partialErr)
			}
			if skippedErr != nil {
				return failErr(1, skippedErr)
			}
			if err := finalizeStorageDestroyCompletion(ctx.OwnershipDir, ctx.Name, runLogPath, plan.State, ledger, skipUnreachable); err != nil {
				return failErr(1, fmt.Errorf("finalize storage teardown completion: %w", err))
			}
			renderResult = result
		default:
			runResult, destroyLogPath, derr := converge.ExecuteDestroy(runContext, stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, false, become.PasswordFile, dryRun, false, workflowLabel, reporter, runLease, true, invocation.args())
			if derr != nil {
				return failErr(1, derr)
			}
			if err := requireSharedServiceMutationLease(sharedServiceLease, "before destroy completion"); err != nil {
				return failErr(1, err)
			}
			if !dryRun && !plan.NoRemoteWork {
				printWorkflowEnd(stdout, workflowLabel)
				cliout.NewContinuation(stdout).Fields([]cliout.Field{{Key: "Destroy log", Value: destroyLogPath}})
			}
			renderResult = runResult.Render
		}
		printRenderResult(stdout, renderResult)
		printBundlePath(stdout, bundle.Dir)
		return nil
	}
	return cmd
}
