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
		flags           scopeCommonFlags
		dryRun          bool
		askBecomePass   bool
		yes             bool
		override        bool
		forceUnowned    bool
		skipUnreachable bool
		cephRecovery    string
		purgeHistory    bool
		verbose         bool
		stage           string
		machinesScope   string
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
	cmd.Flags().BoolVar(&override, "force", false, "authorize protected destroy or otherwise unsafe Bootwright-owned destroy operations; does not imply --yes")
	cmd.Flags().BoolVar(&forceUnowned, "include-unowned", false, "tear down machine VMs (libvirt/KubeVirt/vSphere) that match the Bootwright naming but carry no confirming ownership marker; use after the desired-state names changed post-apply. Does not relax the Ceph ownership gates or device data-safety checks, and does not imply --yes")
	cmd.Flags().BoolVar(&skipUnreachable, "skip-unreachable", false, "tolerate powered-off/unreachable nodes during teardown: skip them (their devices are NOT wiped and local state remains) and continue, leaving the cluster partially destroyed. Requires --force. Storage teardown still fails closed if a cluster's Ceph seed host is unreachable, so ownership stays proven before any device wipe")
	cmd.Flags().StringVar(&cephRecovery, "recover-ceph-ownership", "", "recover missing Ceph controller and host ownership evidence before destroy, as comma-separated <StorageCluster>=<fsid> entries; requires a matching selected managed cluster and an exact /etc/ceph/ceph.conf fsid match, and refuses contradictory controller records. Does not bypass OSD-device safety checks or imply --force or --yes")
	cmd.Flags().BoolVar(&purgeHistory, "purge-history", false, "once a cluster's or machine's teardown succeeds, also delete its retained history: the installer working directory, install/connection records and kubeconfig, and its per-run task and flow logs under this context's runs/ tree. Scoped identically to --clusters/--machines (the whole context on an unscoped destroy); never touches a component outside that scope, a partially-destroyed cluster kept for retry, or an unrelated run's shared ledger. Does not remove the destroy-authorization substrate-release record or the context's ownership/input-history stores")
	addVerboseFlag(cmd, &verbose)
	if options.stageSelector {
		flags.executable = workspace.ResolveAnsiblePlaybook()
		addOutputFlagDryRun(cmd, &flags.output)
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("stage to destroy: %s (sub-phases %s are apply-only); default: full teardown of clusters then infra", strings.Join(converge.DestroyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.DestroyStageNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy (default: all); implies --stage clusters when --stage is omitted; with --stage infra, the literal artifact-server removes only the generated artifact publication service")
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
		if skipUnreachable && !override {
			return failErr(2, errors.New("--skip-unreachable requires --force"))
		}
		runScope := scope
		runCommandLabel := commandLabel
		if options.stageSelector {
			if strings.TrimSpace(stage) == "" && strings.TrimSpace(flags.clusterScope) != "" {
				stage = "clusters"
			}
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
		if forceUnowned && !converge.ScopeTearsMachineLayer(runScope) {
			return failErr(2, errors.New("--include-unowned relaxes machine-substrate ownership refusals, which run only in the infra stage; re-run with --stage infra (optionally with --clusters) or as a full destroy"))
		}
		confirmedCephFSIDs, err := converge.ParseDestroyCephOwnershipRecovery(cephRecovery)
		if err != nil {
			return failErr(2, err)
		}
		if len(confirmedCephFSIDs) > 0 && !converge.ScopeTearsClusterLayer(runScope) {
			return failErr(2, errors.New("--recover-ceph-ownership runs only with the clusters stage or a full destroy"))
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
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
		if len(ownershipSkipped) > 0 && !override && !dryRun {
			details := make([]string, 0, len(ownershipSkipped))
			for _, warning := range ownershipSkipped {
				details = append(details, warning.Error())
			}
			return failErr(1, fmt.Errorf("%d ownership record(s) under %s could not be read and their resources would be silently left standing: %s; fix or remove the corrupted record file(s), or re-run with --force to destroy the rest without them", len(ownershipSkipped), ctx.OwnershipDir, strings.Join(details, "; ")))
		}
		if err := converge.ValidateDestroyCephOwnershipRecovery(sel.RenderState, sel.StorageWorkNames(), ownershipRecords, confirmedCephFSIDs); err != nil {
			return failErr(1, err)
		}
		if runScope.Name == "infra" && sel.Active && !sel.MachineSelection {
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
		var kubeVirtTenantOverrideNotice string
		if sel.Active && len(sel.ContainerRoots) > 0 {
			if conflicts := converge.KubeVirtTenantDestroyConflicts(state, clustersDir, sel.ContainerRoots); len(conflicts) > 0 {
				if !override {
					return failErr(1, converge.FormatKubeVirtTenantConflicts(conflicts))
				}
				kubeVirtTenantOverrideNotice = converge.FormatKubeVirtTenantConflicts(conflicts).Error() + "; proceeding because --force was supplied"
			}
		}
		var storageConsumerOverrideNotice string
		if sel.Active && len(sel.StorageRoots) > 0 {
			if conflicts := stategraph.StorageConsumerDestroyConflicts(state, sel.StorageRoots, sel.ContainerRoots); len(conflicts) > 0 {
				if runScope.Name != "infra" {
					return failErr(1, clusteraccess.FormatStorageConsumerConflicts(conflicts))
				}
				if !override {
					return failErr(1, fmt.Errorf("%w; this infra-stage teardown destroys the storage cluster's machine substrate, losing its OSD data; destroy the consuming cluster(s) first, or re-run with --force to proceed anyway", clusteraccess.FormatStorageConsumerConflicts(conflicts)))
				}
				storageConsumerOverrideNotice = clusteraccess.FormatStorageConsumerConflicts(conflicts).Error() + "; proceeding because --force was supplied"
			}
		}
		if sel.MachineSelection && !override {
			if err := machineDestroyInstalledClusterGuard(clustersDir, sel.ContainerRoots); err != nil {
				return failErr(1, err)
			}
		}
		printPlanStep(stdout, flags.output, runCommandLabel)
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
			decision, blocksErr := converge.PlanInfraComponentDestroyBlocks(ctx.Name, infraComponentServiceRefs(blocksState, artifactServerOnly), ownershipRecords, artifactServerOnly)
			if blocksErr != nil && !override {
				return failErr(1, fmt.Errorf("cannot verify whether shared services are owned or referenced by other contexts: %w; resolve the contexts directory or re-run with --force to tear down regardless", blocksErr))
			}
			if err := converge.InfraComponentDestroyBlockError(decision.Blocks); err != nil && !override {
				return failErr(1, err)
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
		}
		infraScope := !artifactServerOnly && (runScope.Name == "infra" || fullDestroy)
		var resolvedClusterRoots []string
		if infraScope && sel.Active {
			resolvedClusterRoots = sel.AllRoots
		}
		converge.ApplyDestroyScopeExtraVars(&plan, infraScope, flags.clusterScope, resolvedClusterRoots, sel.MachineScopeNames(), forceUnowned, skipUnreachable)
		if err := converge.ApplyDestroyCephOwnershipRecoveryExtraVar(&plan, confirmedCephFSIDs); err != nil {
			return failErr(1, err)
		}
		converge.ApplyVerboseExtraVar(&plan, verbose)
		safetyScope := workflow.DestroySafetyScope{}
		if !artifactServerOnly {
			safetyScope.TearsMachines = converge.ScopeTearsMachineLayer(runScope)
			safetyScope.TearsClusters = converge.ScopeTearsClusterLayer(runScope)
		}
		destroySafety := workflow.EvaluateDestroySafety(plan.State, override, plan.StorageWorkNames, safetyScope)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			if !artifactServerOnly {
				tasks, terr := workflow.PlanDestroyTasks(runScope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
				if terr != nil {
					return failErr(1, terr)
				}
				return runFullDestroyDryRunJSON(stdout, cf, runScope, plan, tasks, converge.DestroyDryRunSafetyReport(destroySafety, override))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, "destroy", plan.State, plan.Selected, playbook, plan.Limit, plan.ExtraVarPairs, artifactsBaseName, false, plan.AskBecomePass, false, workflow.ConcurrencyLimits{}, nil, converge.DestroyDryRunSafetyReport(destroySafety, override), nil, 0)
		}
		if !dryRun && destroySafety.RequiredOverride {
			return failErr(1, fmt.Errorf("%s requires --force for destroy", destroySafety.Summary()))
		}
		if !dryRun {
			if err := checkCurrentApplyBeforeMutation(ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
		}
		printDestroySafety(stdout, destroySafety, override, dryRun)
		if kubeVirtTenantOverrideNotice != "" {
			cliout.NewContinuation(stdout).Warning("kubevirt tenants", kubeVirtTenantOverrideNotice)
		}
		if storageConsumerOverrideNotice != "" {
			cliout.NewContinuation(stdout).Warning("storage consumers", storageConsumerOverrideNotice)
		}
		if len(confirmedCephFSIDs) > 0 {
			cliout.NewContinuation(stdout).Warning("ceph ownership recovery", "before teardown, Bootwright will reconstruct the selected cluster's controller record and host marker only where /etc/ceph/ceph.conf on the declared seed exactly matches the supplied fsid; contradictory controller ownership evidence is refused")
		}
		printSkippedOwnershipRecords(stdout, ownershipSkipped)
		if artifactServerOnly {
			printDestroyArtifactServerPreview(stdout, plan.State)
		} else {
			printDestroyPreview(stdout, runScope, clustersDir, plan.State, plan.StorageWorkNames)
			printDestroyOrphans(stdout, workflow.OwnershipOrphans(state, ownershipRecords))
		}
		printInfraComponentDestroyBlocks(stdout, componentDecision, override)
		printDestroySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		storageScopeNames := converge.DestroyStorageScopeNames(plan.State, plan.StorageWorkNames)
		storagePlanned := workflow.DestroyScopeCoversStorage(runScope.Name) && len(storageScopeNames) > 0
		if !dryRun && !yes && !plan.NoRemoteWork {
			if storagePlanned {
				cliout.NewContinuation(stdout).Warning("data loss", "destroying storage cluster(s) "+strings.Join(storageScopeNames, ", ")+": cephadm rm-cluster --zap-osds destroys ALL OSD DATA and declared devices are wiped (wipefs + sgdisk --zap-all). This is irreversible.")
			}
			if purgeHistory {
				cliout.NewContinuation(stdout).Warning("purge history", "on success this also deletes the destroyed component(s)' installer working directory, install records, kubeconfig, and per-run task/flow logs under runs/ — this history is not recoverable")
			}
			if !confirm(stdin, stdout, destroyConfirmPrompt(storagePlanned)) {
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
			reporter.DryRunTasks(runCommandLabel, workflow.TaskLedgerEntries(tasks), workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks))
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
				partialErr = fmt.Errorf("the storage teardown ran with --skip-unreachable but produced no completion report; keeping the converge records of storage cluster(s) %s — re-run destroy to verify their teardown", strings.Join(storageScopeNames, ", "))
			}
			resetPartial := partial.Recorded
			if partialErr != nil {
				resetPartial = storageScopeNames
			}
			if gerr != nil {
				printPartialStorageDestroyWarning(stdout, partial, partialErr)
				printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, workflow.SucceededDestroyTaskKinds(ledger), purgeHistory, skipUnreachable)
				if ledger.Status == workflow.RunStatusFailed && (len(ledger.FailedTasks()) > 0 || len(ledger.BlockedTasks()) > 0) {
					return silentExit(1)
				}
				return failErr(1, gerr)
			}
			printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, resetPartial, nil, purgeHistory, skipUnreachable)
			printPartialStorageDestroyWarning(stdout, partial, partialErr)
			renderResult = result
		default:
			runResult, destroyLogPath, derr := converge.ExecuteDestroy(c.Context(), stdout, stderr, ctx, clustersDir, flags.executable, bundle.Dir, playbook, plan, artifactsBaseName, false, become.PasswordFile, dryRun, false, workflowLabel, reporter)
			if derr != nil {
				return failErr(1, derr)
			}
			if !dryRun && !artifactServerOnly && !plan.NoRemoteWork {
				printDestroyRecordReset(stdout, sel, ctx.RunsDir, clustersDir, ctx.Name, runScope, plan, nil, nil, purgeHistory, skipUnreachable)
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
