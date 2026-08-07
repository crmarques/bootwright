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
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

type scopeDestroyOptions struct {
	use           string
	short         string
	long          string
	example       string
	stageSelector bool
	commandLabel  string
}

func newScopeDestroyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeDestroyOptions) *cobra.Command {
	var (
		flags         scopeCommonFlags
		dryRun        bool
		askBecomePass bool
		yes           bool
		authorizeFlag []string
		cephRecovery  string
		purgeHistory  bool
		verbose       bool
		stage         string
		machinesScope string
	)
	use := "destroy"
	if options.use != "" {
		use = options.use
	}
	short := "Destroy " + scope.Name + " runtime state"
	if options.short != "" {
		short = options.short
	}
	example := options.example
	commandLabel := scope.Name + " destroy"
	if options.commandLabel != "" {
		commandLabel = options.commandLabel
	}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    options.long,
		Args:    cobra.NoArgs,
		Example: example,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, flagDryRunUsage)
	addAskBecomePassFlag(cmd, &askBecomePass)
	addYesFlag(cmd, &yes, "destroy")
	addAuthorizeFlag(cmd, &authorizeFlag, authorizeVerbDestroy)
	cmd.Flags().StringVar(&cephRecovery, "recover-ceph-ownership", "", "recover missing Ceph controller and host ownership evidence before destroy, as comma-separated <StorageCluster>=<fsid> entries; requires a matching selected managed cluster and an exact /etc/ceph/ceph.conf fsid match, and refuses contradictory controller records. Does not bypass OSD-device safety checks and authorizes no named risk of its own")
	cmd.Flags().BoolVar(&purgeHistory, "purge-history", false, "once a cluster's or machine's teardown succeeds, also delete its retained history: its whole state tree under this context's clusters/<name>/ (installer working directory, install/connection records, kubeconfig, captured cluster secrets) and its per-run task and flow logs under runs/. Scoped identically to --clusters/--machines; a fully successful unscoped destroy instead sweeps the context's whole run history — every archived run, earlier destroy attempts, and crashed runs that never archived a ledger — keeping only this destroy run's own record. Never touches a component outside that scope, a partially-destroyed cluster kept for retry, an unrelated run's shared ledger, or the provider state of a machine layer this run leaves standing (--stage clusters). Does not remove the destroy-authorization substrate-release record or the context's ownership/input-history stores")
	addVerboseFlag(cmd, &verbose)
	if options.stageSelector {
		flags.executable = workspace.ResolveAnsiblePlaybook()
		addOutputFlagDryRun(cmd, &flags.output)
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("stage to destroy: %s (sub-phases %s are apply-only); default: full lifecycle teardown for the selected work set", strings.Join(converge.DestroyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.DestroyStageNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy (default: all); without --stage, tears down the selected clusters and their exclusively owned infrastructure; with --stage infra, the literal artifact-server removes only the generated artifact publication service")
		registerClusterScopeCompletion(cmd, clusterKindAny)
		cmd.Flags().StringVar(&machinesScope, "machines", "", flagMachinesDestroyUsage)
		registerMachineScopeCompletion(cmd)
	} else {
		registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, true), "destroy")
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		auth, err := parseAuthorizations(authorizeFlag, authorizeVerbDestroy)
		if err != nil {
			return failErr(2, err)
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			var err error
			runScope, err = converge.DestroyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = converge.DestroyStageCommandLabel(stage, commandLabel)
		}
		if machinesScope != "" {
			var merr error
			if runScope, runCommandLabel, merr = machineDestroyScope(flags.clusterScope, stage); merr != nil {
				return merr
			}
		}
		fullDestroy := converge.DestroyIsFullScope(runScope)
		forceUnowned, forceUnownedNetworks := false, false
		if converge.ScopeTearsMachineLayer(runScope) {
			forceUnowned = auth.allows(authorizeUnownedVMs)
			forceUnownedNetworks = auth.allows(authorizeUnownedNetworks)
		}
		confirmedCephFSIDs, err := converge.ParseDestroyCephOwnershipRecovery(cephRecovery)
		if err != nil {
			return failErr(2, err)
		}
		if len(confirmedCephFSIDs) > 0 && !converge.ScopeTearsClusterLayer(runScope) {
			return failErr(2, errors.New("--recover-ceph-ownership runs only with the clusters stage or a full destroy"))
		}
		ctx, localitySkipped, err := cf.resolveTolerantInput()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		var state v1alpha1.State
		state, stateSkipped, err := loadDesiredStateTolerant(cf)
		if err != nil {
			return failErr(1, err)
		}
		inputSkipped := mergeSkippedInputDocuments(localitySkipped, stateSkipped)
		staleInputReached := len(inputSkipped) > 0
		if staleInputReached && !auth.allows(authorizeStaleInput) {
			return failErr(1, staleInputRefusal(ctx.Name, inputSkipped))
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
		ownershipRecords, ownershipSkipped, err := converge.LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		unreadableRecordsReached := len(ownershipSkipped) > 0
		if unreadableRecordsReached && !dryRun && !auth.allows(authorizeUnreadableRecords) {
			return failErr(1, unreadableOwnershipRefusal(ctx, ownershipSkipped))
		}
		if err := converge.ValidateDestroyCephOwnershipRecovery(sel.RenderState, sel.StorageWorkNames(), ownershipRecords, confirmedCephFSIDs); err != nil {
			return failErr(1, err)
		}
		if (runScope.Name == "infra" || fullDestroy) && sel.Active && !sel.MachineSelection {
			conflicts := stategraph.SharedDestroyConflicts(state, sel.AllRoots)
			if len(conflicts) > 0 {
				if standingTasks, perr := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), state); perr == nil {
					conflicts = workflow.StandingDestroyScopeConflicts(ctx.RunsDir, clustersDir, state, ownershipRecords, standingTasks, conflicts)
				}
			}
			if len(conflicts) > 0 {
				return failErr(1, clusteraccess.FormatDestroyScopeConflicts(conflicts, "--clusters"))
			}
		}
		if sel.Active && len(sel.ContainerRoots) > 0 {
			if conflicts := converge.KubeVirtTenantDestroyConflicts(state, clustersDir, sel.ContainerRoots); len(conflicts) > 0 {
				return failErr(1, converge.FormatKubeVirtTenantConflicts(conflicts))
			}
		}
		sharedInfraReached, storageConsumerOverrideNotice, consumerErr := destroyStorageConsumerGate(auth, state, sel, runScope, dryRun)
		if consumerErr != nil {
			return failErr(1, consumerErr)
		}
		installedClusterNodeReached := false
		if sel.MachineSelection {
			if err := machineDestroyInstalledClusterGuard(clustersDir, sel.ContainerRoots, sel.StorageRoots, ownershipRecords); err != nil {
				installedClusterNodeReached = true
				if !auth.allows(authorizeInstalledClusterNode) && !dryRun {
					return failErr(1, err)
				}
			}
		}
		playbook := runScope.DestroyPlaybook
		artifactsBaseName := runScope.ArtifactsBaseName + "-destroy"
		workflowLabel := runCommandLabel
		tearsDownInfraComponents := artifactServerOnly || ((runScope.Name == "infra" || fullDestroy) && !sel.MachineSelection)
		var componentDecision converge.InfraComponentDestroyDecision
		if tearsDownInfraComponents {
			blocksState := sel.RenderState
			if artifactServerOnly {
				blocksState = state
			}
			decision, blocked, blocksErr := destroyInfraComponentGate(auth, ctx.Name, infraComponentServiceRefs(blocksState, artifactServerOnly), ownershipRecords, artifactServerOnly, dryRun)
			sharedInfraReached = sharedInfraReached || blocked
			if blocksErr != nil {
				return failErr(1, blocksErr)
			}
			componentDecision = decision
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
		converge.ApplyDestroyScopeExtraVars(&plan, infraScope, flags.clusterScope, resolvedClusterRoots, sel.MachineScopeNames(), forceUnowned, forceUnownedNetworks, skipUnreachable, destroyAuthorizesUnownedDevices(auth, runScope))
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
		storagePlanned := workflow.DestroyScopeCoversStorage(runScope.Name) && len(storageScopeNames) > 0
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
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			disclosure := destroyDryRunDisclosure(destroySafety, inputSkipped, ownershipSkipped, componentDecision, auth, purgeHistory && !plan.NoRemoteWork)
			return runDestroyDryRunJSON(c, stdout, cf, flags, runScope, plan, playbook, artifactsBaseName, artifactServerOnly, converge.DestroyDryRunSafetyReport(destroySafety, authorizedProtected), requiredAuth, disclosure)
		}
		if !dryRun && destroySafety.RequiresAuthorization {
			return failErr(1, fmt.Errorf("%s; re-run `bootwright destroy --authorize %s` with the same --stage/--clusters selection to destroy it anyway", destroySafety.Summary(), authorizeProtected))
		}
		if !dryRun {
			if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
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
			printDestroyPreview(stdout, runScope, clustersDir, plan.State, plan.StorageWorkNames)
			printDestroyOrphans(stdout, workflow.OwnershipOrphans(state, ownershipRecords))
		}
		printInfraComponentDestroyBlocks(stdout, componentDecision, auth.has(authorizeSharedInfra))
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
		if dryRun {
			printRequiredAuthorizations(stdout, requiredAuth)
		}
		warnUnusedAuthorizations(stdout, auth, dryRun)
		if dataLossPlanned {
			if err := destroyDataLossYesGuard(dataLoss, yes, allowDestroy); err != nil {
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
		useGraph := !dryRun && !plan.NoRemoteWork && !artifactServerOnly
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
			result, ledger, runLogPath, gerr := converge.ExecuteDestroyGraph(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, runScope.Name, flags.clusterScope, plan, false, become.PasswordFile, false, workflowLabel, dr)
			partial, partialErr := converge.RecordPartialStorageDestroy(ctx.OwnershipDir, ctx.Name, runLogPath)
			if gerr == nil && partialErr == nil && storagePlanned && skipUnreachable && !partial.Found {
				partialErr = fmt.Errorf("the storage teardown ran with --authorize unreachable-nodes but produced no completion report; keeping the converge records of storage cluster(s) %s — re-run destroy to verify their teardown", strings.Join(storageScopeNames, ", "))
			}
			resetPartial := partial.Clusters
			if partialErr != nil {
				resetPartial = storageScopeNames
			}
			if gerr != nil {
				printPartialStorageDestroyWarning(stdout, partial, partialErr)
				_ = printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, workflow.SucceededDestroyTaskKinds(ledger), ledger.RunID, purgeHistory, skipUnreachable)
				if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
					return silentExit(1)
				}
				return failErr(1, gerr)
			}
			resetErr := printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, nil, ledger.RunID, purgeHistory, skipUnreachable)
			printPartialStorageDestroyWarning(stdout, partial, partialErr)
			if resetErr != nil {
				return failErr(1, resetErr)
			}
			if partialErr != nil {
				return failErr(1, partialErr)
			}
			renderResult = result
		default:
			runResult, destroyLogPath, derr := converge.ExecuteDestroy(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, false, become.PasswordFile, dryRun, false, workflowLabel, reporter)
			if derr != nil {
				return failErr(1, derr)
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
