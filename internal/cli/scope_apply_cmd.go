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
		strictSecrets   bool
		override        bool
		allowDestroy    bool
		expectNew       bool
		trustOnFirstUse bool
		verbose         bool
		parallelism     int
		perHost         int
		redfish         int
		stage           string
		through         string
		reclaimDevices  string
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
	cmd.Flags().BoolVar(&strictSecrets, "strict-secrets", false, flagStrictSecretsUsage)
	addVerboseFlag(cmd, &verbose)
	cmd.Flags().BoolVar(&expectNew, "expect-new", false, "assert a greenfield run: fail if any selected object already exists; without it apply reconciles (creates what is missing, skips what matches, fails closed on drift)")
	if converge.ScopeTargetsContainerInstall(scope) {
		cmd.Flags().BoolVar(&override, "override", false, "authorize Bootwright-owned destructive rebuilds (rebuild drifted owned objects, managed-OS VM reinstall, owned-Ceph wipe-and-rebuild); never touches foreign objects, and skips objects already matching desired state; mutually exclusive with --expect-new")
		cmd.Flags().BoolVar(&allowDestroy, "allow-destroy", false, "authorize a destructive --override rebuild (machine reinstall with disks wiped, Ceph OSD zap) — required alongside --yes for such a rebuild, and pre-accepts the interactive data-loss prompt; --yes alone never authorizes data loss")
	}
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "maximum concurrent apply tasks (0 auto safe maximum)")
	if usesAnsible {
		cmd.Flags().IntVar(&perHost, "parallelism-per-host", 0, "maximum concurrent mutating tasks per provider host (0 auto safe maximum)")
		cmd.Flags().IntVar(&redfish, "parallelism-redfish", 0, "maximum concurrent Redfish boot tasks (0 auto safe maximum)")
		cmd.Flags().StringVar(&reclaimDevices, "reclaim-devices", "", "comma-separated block-device paths to WIPE in-band before a managed-Ceph apply (recover owned OSD disks whose on-node marker was lost by a managed-OS reinstall); only wipes a named device that is a declared OSD device of a Bootwright-owned cluster and is not mounted or a system disk — irreversible data loss")
	}
	if options.stageSelector {
		if usesAnsible {
			flags.executable = workspace.ResolveAnsiblePlaybook()
		}
		addOutputFlagDryRun(cmd, &flags.output)
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("stage to %s: %s (or sub-phase %s); default: full graph", action, strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.ApplyStageNames())
		cmd.Flags().StringVar(&through, "through", "", fmt.Sprintf("limit %s to all stages up to and including STAGE: %s (or sub-phase %s); cumulative, excludes --stage", action, strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerFlagCompletion(cmd, "through", converge.ApplyStageNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to apply (default: all)")
	} else {
		registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), action, usesAnsible, scopeTargetKind(scope))
	}
	if options.defaultPlan {
		if flag := cmd.Flags().Lookup("output"); flag != nil {
			flag.Usage = flagOutputUsage
		}
	}
	if options.hideExecFlags {
		for _, name := range []string{"reclaim-devices", "allow-destroy", "ask-become-pass", "strict-secrets", "verbose"} {
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
			return failErr(2, errors.New("--expect-new and --override are mutually exclusive: --expect-new asserts nothing exists yet, --override rebuilds drift"))
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
			if stage != "" && through != "" {
				return failErr(2, errors.New("--stage and --through are mutually exclusive: --stage runs exactly that phase, --through runs every phase from the beginning up to and including it"))
			}
			var err error
			if through != "" {
				runScope, err = converge.ApplyThroughScope(through)
				runCommandLabel = converge.ApplyThroughCommandLabel(through, action, commandLabel)
			} else {
				runScope, err = converge.ApplyStageScope(stage)
				runCommandLabel = converge.ApplyStageCommandLabel(stage, action, commandLabel)
			}
			if err != nil {
				return failErr(2, err)
			}
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		if strictSecrets {
			if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
				return e
			}
		}
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		printMutatingRunPreamble(stdout, flags.output, runCommandLabel)
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		printPlanStep(stdout, flags.output, runCommandLabel)
		sel, err := clusteraccess.Resolve(state, runScope.Name, flags.clusterScope)
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
		if mode == workflow.ApplyModeOverride && stage == converge.PhaseBase && len(sel.StorageWorkNames()) > 0 {
			return failErr(2, errors.New("--override --stage base skips the deps-phase device-empty gate that must precede a Ceph wipe-and-rebuild; use --override --through base (runs the gate then the rebuild) or the full graph"))
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
		applyTarget, tasks, limits, dryRunTasks, err := converge.PlanScopedApply(runScope, &plan, mode, sel.StorageWorkNames(), sel.Active, workflow.ConcurrencyLimits{
			Parallelism:        parallelism,
			ParallelismPerHost: perHost,
			ParallelismRedfish: redfish,
		}, ctx.RunsDir)
		if err != nil {
			return failErr(1, err)
		}
		converge.ApplyVerboseExtraVar(&plan, verbose)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, action, plan.State, plan.Selected, runScope.ApplyPlaybook, plan.Limit, plan.ExtraVarPairs, runScope.ArtifactsBaseName, false, plan.AskBecomePass, plan.TargetsClusters, limits, dryRunTasks, nil, workflow.AnsibleForksForLimit(plan.State, plan.Limit))
		}
		var destructiveOverride []string
		if !dryRun {
			objects, err := converge.ApplyModePreflight(mode, tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, err)
			}
			if err := converge.CheckApplyRenameOrphan(plan.State, objects, clustersDir, sel.Active); err != nil {
				return failErr(1, err)
			}
			if override {
				if err := converge.CheckApplyOverrideDestroyProtection(plan.State, objects); err != nil {
					return failErr(1, err)
				}
				destructiveOverride = workflow.OverrideDestructiveDriftedObjects(objects)
			}
			if reclaimDevices != "" {
				destructiveOverride = append(destructiveOverride, reclaimDestructiveDescriptors(reclaimDevices, converge.OwnedStorageClusters(objects))...)
			}
			if err := destructiveOverrideYesGuard(destructiveOverride, yes, allowDestroy); err != nil {
				return failErr(1, err)
			}
			emitApplyDataLossWarningsAndVars(stdout, mode, objects, tasks, &plan, reclaimDevices)
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			hostTrustScope := workflow.ApplyTaskConnectedMachines(tasks)
			if trustOnFirstUse && !yes && flags.output == outputText {
				if err := offerTrustOnFirstUse(c.Context(), stdin, stdout, ctx.BaseDir, plan.State, defaultHostTrustDeps, hostTrustScope); err != nil {
					return failErr(1, err)
				}
			}
			if err := runApplyHostCheck(stdout, stderr, plan.State, plan.Selected, ctx.Name, ctx.SecretsDir, clustersDir, hostTrustScope, secretScope); err != nil {
				return err
			}
		}
		printApplySummary(stdout, plan.Selected, plan.AskBecomePass, dryRun, plan.NoRemoteWork)
		if !dryRun && !yes && !plan.NoRemoteWork {
			if !confirm(stdin, stdout, destructiveApplyConfirmPrompt(stdout, destructiveOverride, allowDestroy)) {
				return failErr(1, errors.New("apply aborted"))
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
		if dryRun {
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight "+runScope.Name+" to validate secrets, tools, and remote readiness")
			if options.stageSelector {
				printStageScopeNotices(stdout, runScope)
			}
			reporter.DryRunTasks(runCommandLabel, workflow.TaskLedgerEntries(dryRunTasks), limits)
			printApplyTransitionLedger(stdout, tasks, ctx.RunsDir, mode)
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
		if plan.TargetsClusters {
			printClusterAccess(stdout, plan.State, renderResult, ledger, clustersDir)
		}
		return nil
	}
	return cmd
}
