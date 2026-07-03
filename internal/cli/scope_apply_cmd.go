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
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(runCommandLabel)
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		var state v1alpha1.State
		state, err = loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		// Resolve the cluster selection once: the render state to plan over, the
		// storage roots actually provisioned, and the readiness work objects. A
		// managed StorageCluster pulled into the render state only as a render
		// reference for a selected container cluster's data-foundation attachment
		// is left out of both the provisioning set and the readiness checks, so
		// scoping a container cluster never requires its external storage's
		// bootstrap secrets or host trust (ADR-0004). This holds for every apply
		// command that accepts --clusters, not only the stage-selector apply.
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
		// The marker-aware device-empty gate for managed Ceph runs in the deps
		// phase, while the destructive `cephadm rm-cluster --zap-osds` rebuild runs
		// in the base phase; `--stage base` plans only the base seed task, so the
		// device gate never executes before the wipe-and-rebuild. Refuse
		// `--override --stage base` for a scope carrying a managed StorageCluster and
		// point at `--through base`, which includes the deps-phase gate.
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
		// Stamp the verbose-output gate after PlanScopedApply has composed
		// plan.ExtraVarPairs so it flows to both the --dry-run JSON command
		// preview and the real run (BuildApplyRunOptions copies ExtraVarPairs).
		converge.ApplyVerboseExtraVar(&plan, verbose)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, action, plan.State, plan.Selected, runScope.ApplyPlaybook, plan.Limit, plan.ExtraVarPairs, runScope.ArtifactsBaseName, false, plan.AskBecomePass, plan.TargetsClusters, limits, dryRunTasks, nil, workflow.AnsibleForksForLimit(plan.State, plan.Limit))
		}
		if !dryRun {
			objects, err := converge.ApplyModePreflight(mode, tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, err)
			}
			// apply --override authorizes Bootwright-owned destructive rebuilds
			// (managed-OS VM reinstall, owned-Ceph wipe-and-rebuild, cluster
			// reinstall). On a destroy-protected Environment that destruction must
			// cross the destroy authorization boundary instead of slipping in through
			// apply, so fail closed before any mutation and direct the operator to
			// destroy first. Scope/drift-aware (keyed on the classified objects): a
			// scoped apply whose only drift is a reconfigure-only fabric service is
			// not blocked. Dry-run/plan still previews the override plan.
			if override {
				if err := converge.CheckApplyOverrideDestroyProtection(plan.State, objects); err != nil {
					return failErr(1, err)
				}
			}
			// --override rebuilds drifted storage sub-objects; a structural change
			// (pool type/erasure profile, CephFS metadata or default data pool) is
			// data-destroying. Warn before the confirm prompt so the operator sees
			// which pools/filesystems are at risk.
			if mode == workflow.ApplyModeOverride {
				if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
					cliout.NewContinuation(stdout).Warning("override", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
				}
				if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
					cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
				}
				// A StorageCluster whose only drift is a reconcilable OSD-device add is
				// reconciled in place by the seed role, not wiped: pass its name so the
				// override apply-mode gate suppresses rm-cluster --zap-osds for it.
				converge.ApplyReconcilableOnlyStorageExtraVar(&plan, converge.ReconcilableOnlyStorageClusters(objects))
			}
			// --reclaim-devices wipes the named OSD disks in-band before the apply's
			// empty-device gate, to recover owned OSD disks whose on-node marker a
			// managed-OS reinstall erased. It is gated in Ansible on the device being a
			// declared OSD device of a controller-owned cluster and not mounted or a
			// system disk; here it emits the irreversible-data-loss warning and threads
			// the owned-cluster allowlist so a foreign/greenfield cluster's disks are
			// never reclaimed.
			if reclaimDevices != "" {
				owned := converge.OwnedStorageClusters(objects)
				if len(owned) == 0 {
					cliout.NewContinuation(stdout).Warning("reclaim", "--reclaim-devices was given but no selected StorageCluster is recorded as Bootwright-owned; no device will be reclaimed (reclaim only wipes disks of an owned cluster).")
				} else {
					cliout.NewContinuation(stdout).Warning("reclaim", "will WIPE device(s) "+reclaimDevices+" on the owned Ceph cluster(s) "+strings.Join(owned, ", ")+" before apply — IRREVERSIBLE data loss. Only a named device that is a declared OSD device and is not mounted or a system disk is wiped.")
				}
				converge.ApplyReclaimDevicesExtraVars(&plan, reclaimDevices, owned)
			}
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			// Host trust is required only for machines the planned tasks will
			// actually SSH into. A scoped run can pull an object into plan.State
			// purely as a render reference (e.g. a managed StorageCluster pulled in
			// by an OCP cluster's data-foundation attachment); its provided-OS nodes
			// that no selected phase connects to must not block the run on missing
			// trust. When a phase that connects to them is in scope its tasks select
			// them and trust is required again.
			hostTrustScope := workflow.ApplyTaskConnectedMachines(tasks)
			// Trust-on-first-use: only in interactive text runs, and only for
			// hosts with no recorded key. --yes and JSON runs fail closed on
			// missing trust exactly as before.
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
			if !confirm(stdin, stdout, "Continue with apply? [y/N] (default: no): ") {
				return failErr(1, errors.New("apply aborted"))
			}
		}
		// The single "Run" section is opened by the workflow reporter as it
		// reports render/bundle prep, then printApplyRunStart adds the run
		// identity fields under it — no separate workflow-start banner.
		become := becomeCredential{}
		if !dryRun && !plan.NoRemoteWork && willPromptForBecomePassword(plan.AskBecomePass) {
			cliout.NewContinuation(stderr).BlankLine()
		}
		if !dryRun && !plan.NoRemoteWork {
			credential, cleanup, err := prepareBecomeCredential(stdin, stderr, plan.AskBecomePass, false, true)
			if err != nil {
				return failErr(1, err)
			}
			defer cleanup()
			become = credential
		}
		reporter := newWorkflowReporter(stdout)
		if plan.AskBecomePass && become.PasswordFile == "" {
			reporter.WithPromptGap(stderr)
		}
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
			printExtensionDryRun(stdout, dryRunTasks)
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
