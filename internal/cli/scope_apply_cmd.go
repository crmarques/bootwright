package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type scopeApplyOptions struct {
	use           string
	short         string
	example       string
	defaultPlan   bool
	hideDryRun    bool
	hideApproval  bool
	stageSelector bool
	commandLabel  string
	action        string
}

func newScopeApplyCmdWithOptions(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeApplyOptions) *cobra.Command {
	usesAnsible := scopeUsesAnsible(scope)
	var (
		flags         scopeCommonFlags
		dryRun        = options.defaultPlan
		check         bool
		askBecomePass bool
		yes           bool
		strictSecrets bool
		override      bool
		expectNew     bool
		parallelism   int
		perHost       int
		redfish       int
		stage         string
	)
	use := "apply"
	if options.use != "" {
		use = options.use
	}
	short := "Apply " + scope.name + " desired state"
	if options.short != "" {
		short = options.short
	}
	example := ""
	if options.example != "" {
		example = options.example
	}
	commandLabel := scope.name + " apply"
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
		Args:    cobra.NoArgs,
		Example: example,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", options.defaultPlan, "render artifacts and print a plan only; does not run readiness checks or mutate remote systems")
	if options.hideDryRun {
		_ = cmd.Flags().MarkHidden("dry-run")
	}
	if usesAnsible {
		cmd.Flags().BoolVar(&check, "check", false, "pass --check to ansible-playbook")
		cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the Ansible become password; defaults to false when bootwright runs as root, true otherwise")
	}
	if !options.hideApproval {
		cmd.Flags().BoolVar(&yes, "yes", false, "skip the apply confirmation prompt")
	}
	cmd.Flags().BoolVar(&strictSecrets, "strict-secrets", false, "abort if context secrets-dir mode is not 0700 or any secret file mode is not 0600 (default: warn only)")
	cmd.Flags().BoolVar(&expectNew, "expect-new", false, "assert a greenfield run: fail if any selected object already exists; without it apply reconciles (creates what is missing, skips what matches, fails closed on drift)")
	if scopeTargetsContainerInstall(scope) {
		cmd.Flags().BoolVar(&override, "override", false, "authorize Bootwright-owned destructive rebuilds (rebuild drifted owned objects, managed-OS VM reinstall, owned-Ceph wipe-and-rebuild); never touches foreign objects, and skips objects already matching desired state; mutually exclusive with --expect-new")
	}
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "maximum concurrent apply tasks (0 auto safe maximum)")
	if usesAnsible {
		cmd.Flags().IntVar(&perHost, "parallelism-per-host", 0, "maximum concurrent mutating tasks per provider host (0 auto safe maximum)")
		cmd.Flags().IntVar(&redfish, "parallelism-redfish", 0, "maximum concurrent Redfish boot tasks (0 auto safe maximum)")
	}
	if options.stageSelector {
		flags.output = outputText
		if usesAnsible {
			cmd.Flags().StringVar(&flags.executable, "ansible-playbook", resolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
		}
		cmd.Flags().StringVar(&flags.output, "output", flags.output, "output format: text|json (json is supported with --dry-run)")
		cmd.Flags().StringVar(&stage, "stage", "", "stage to apply: infra|clusters families, or a sub-phase fabric|machines|deps|base|addons (default full graph)")
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to apply")
	} else {
		registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), action, usesAnsible, scopeTargetKind(scope))
	}
	if options.defaultPlan {
		if flag := cmd.Flags().Lookup("output"); flag != nil {
			flag.Usage = "output format: text|json"
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
			var err error
			runScope, err = applyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = applyStageCommandLabel(stage, action, commandLabel)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := controllerClustersDir(ctx.Name)
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
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		storageSelectionActive := false
		if options.stageSelector && strings.TrimSpace(flags.clusterScope) != "" {
			// Validate the requested names here; the effective storage selection is
			// derived from the scoped plan.state below so it includes
			// data-foundation attachment targets pulled in transitively.
			if _, _, err := clusterRootNamesForTarget(state, flags.clusterScope); err != nil {
				return failErr(1, err)
			}
			storageSelectionActive = true
		}
		if err := validateScopedApplySharedServices(state, runScope.name, flags.clusterScope); err != nil {
			return failErr(1, err)
		}
		if options.stageSelector {
			if err := validateKubeVirtClusterSelection(state, runScope, flags.clusterScope, clustersDir); err != nil {
				return failErr(1, err)
			}
		}
		plan, err := prepareScopedApplyWorkflow(state, runScope, flags.clusterScope, askBecomePass, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if scopeUsesAnsible(runScope) {
			if err := workflow.EnsureApplySupported(plan.state); err != nil {
				return failErr(1, err)
			}
		}
		if override {
			// apply --override authorizes Bootwright-owned destructive rebuilds
			// (managed-OS VM reinstall, owned-Ceph wipe-and-rebuild). On a
			// destroy-protected Environment that destruction must cross the destroy
			// authorization boundary instead of slipping in through apply, so fail
			// closed before any mutation and direct the operator to destroy first.
			// Dry-run/plan still previews the override plan.
			if !dryRun {
				if protected := workflow.ProtectedEnvironments(plan.state); len(protected) > 0 {
					return failErr(1, fmt.Errorf("apply --override would rebuild destroy-protected resources (%s); run `bootwright destroy --override` for the affected scope, then re-apply", strings.Join(protected, ", ")))
				}
			}
		}
		// The explicit safety mode drives both the Go object preflight and the
		// per-role Ansible gate (create/continue/override). It replaces the legacy
		// bootwright_install_override boolean.
		plan.extraVarPairs = append(plan.extraVarPairs, "bootwright_apply_mode="+string(mode))
		limits := workflow.ConcurrencyLimits{
			Parallelism:        parallelism,
			ParallelismPerHost: perHost,
			ParallelismRedfish: redfish,
		}
		applyTarget := runScope.applyTarget()
		if storageSelectionActive {
			// plan.state already includes the transitive closure of storage clusters
			// required by the selected container clusters' data-foundation
			// attachments (FilterStateToClusters -> filterStorageToClusters).
			// Schedule provision for all of them; selecting only the literal
			// --clusters storage names would skip a transitively-required managed
			// StorageCluster, failing the attachment task after nodes have booted.
			names := make([]string, 0, len(plan.state.StorageClusters))
			for _, sc := range plan.state.StorageClusters {
				names = append(names, sc.Metadata.Name)
			}
			applyTarget.StorageClusterNames = names
		}
		tasks, err := workflow.PlanApplyTasksChecked(applyTarget, plan.state)
		if err != nil {
			return failErr(1, err)
		}
		limits = workflow.ResolveApplyConcurrencyLimits(limits, tasks)
		dryRunTasks := workflow.AnnotateApplyTaskClusterLogPaths(clustersDir, "dry-run", tasks)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, action, plan.state, plan.selected, runScope.applyPlaybook, plan.limit, plan.extraVarPairs, runScope.artifactsBaseName, check, plan.askBecomePass, plan.targetsClusters, limits, dryRunTasks, nil, workflow.AnsibleForksForLimit(plan.state, plan.limit))
		}
		if !dryRun {
			// Mode preflight: classify every selected object against the recorded
			// convergence state and enforce the apply-mode contract before any
			// mutation (the default reconcile refuses drift/foreign; --expect-new
			// refuses pre-existing objects; --override refuses only foreign). Per-role
			// Ansible gates enforce the same contract against live state.
			objects, err := workflow.ClassifyApplyObjects(tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, err)
			}
			if err := workflow.EvaluateApplyModePreflight(mode, objects); err != nil {
				return failErr(1, err)
			}
			// --override rebuilds drifted storage sub-objects; a structural change
			// (pool type, CephFS metadata pool) is data-destroying. Warn before the
			// confirm prompt so the operator sees which pools/filesystems are at risk.
			if mode == workflow.ApplyModeOverride {
				if rebuilt := overrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
					cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type or CephFS metadata pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
				}
			}
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.RunsDir); err != nil {
				return failErr(1, err)
			}
			if err := runApplyHostCheck(stdout, stderr, plan.state, plan.selected, ctx.SecretsDir, clustersDir); err != nil {
				return err
			}
		}
		printApplySummary(stdout, plan.selected, plan.askBecomePass, dryRun, plan.noRemoteWork)
		if !dryRun && !yes && !plan.noRemoteWork {
			if !confirm(stdin, stdout, "Continue with apply? [y/N] (default: no): ") {
				return failErr(1, errors.New("apply aborted"))
			}
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowStart(stdout, runScope.name, plan.selected, plan.askBecomePass)
		}
		become := becomeCredential{}
		if !dryRun && !plan.noRemoteWork && willPromptForBecomePassword(plan.askBecomePass) {
			cliout.NewContinuation(stderr).BlankLine()
		}
		if !dryRun && !plan.noRemoteWork {
			credential, cleanup, err := prepareBecomeCredential(stdin, stderr, plan.askBecomePass, false, true)
			if err != nil {
				return failErr(1, err)
			}
			defer cleanup()
			become = credential
		}
		reporter := newWorkflowReporter(stdout)
		if plan.askBecomePass && become.PasswordFile == "" {
			reporter.WithPromptGap(stderr)
		}
		usesAnsible := scopeUsesAnsible(runScope)
		var bundleResult bundle.AnsibleBundleResult
		if usesAnsible {
			bundleResult, err = prepareWorkflowBundle(true)
			if err != nil {
				return failErr(1, err)
			}
		}
		runOpts := workflow.RunOptions{
			State:              plan.state,
			RenderedDir:        ctx.RenderedDir,
			ClustersDir:        clustersDir,
			RunsDir:            ctx.RunsDir,
			ContextName:        ctx.Name,
			SecretsDir:         ctx.SecretsDir,
			ManagedServicesDir: ctx.ManagedServicesDir,
			ProviderStateDir:   ctx.ProviderStateDir,
			OwnershipDir:       ctx.OwnershipDir,
			Executable:         flags.executable,
			Playbook:           runScope.applyPlaybook,
			Limit:              plan.limit,
			Forks:              workflow.AnsibleForksForLimit(plan.state, plan.limit),
			ExtraVarPairs:      plan.extraVarPairs,
			ArtifactsBaseName:  runScope.artifactsBaseName,
			Check:              check,
			AskBecomePass:      plan.askBecomePass && become.PasswordFile == "",
			BecomePasswordFile: become.PasswordFile,
			UseControllingTTY:  useControllingTTYForWorkflow(plan.selected, plan.askBecomePass && become.PasswordFile == ""),
			DryRun:             dryRun,
			ResolveInstaller:   plan.targetsClusters,
			Label:              runCommandLabel,
			ApplyMode:          mode,
		}
		if dryRun {
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright preflight "+runScope.name+" to validate secrets, tools, and remote readiness")
			reporter.DryRunTasks(runCommandLabel, workflow.TaskLedgerEntries(dryRunTasks), limits)
			printExtensionDryRun(stdout, dryRunTasks)
			result, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.state)
			if err != nil {
				return failErr(1, err)
			}
			printRenderResult(stdout, result)
			if usesAnsible {
				printBundlePath(stdout, bundleResult.Dir)
			}
			return nil
		}
		if reporter != nil {
			reporter.RenderStart()
		}
		renderResult, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.state)
		if err != nil {
			return failErr(1, err)
		}
		prepared, err := workflow.PrepareApplyTaskGraph(c.Context(), ctx.RunsDir, runOpts, tasks, limits)
		if err != nil {
			return failErr(1, err)
		}
		// Record the exact input set this mutating run was launched from as a
		// per-run forensic output next to the run's ledger and task logs.
		if err := snapshotMutatingRunInput(workflow.RunInputSnapshotDir(ctx.RunsDir, prepared.RunID), ctx); err != nil {
			return failErr(1, err)
		}
		if plan.targetsClusters {
			reporter.ResolveInstallerStart()
			if _, err := workflow.ResolveInstallerForContext(ctx.Name, clustersDir, ctx.SecretsDir, plan.state); err != nil {
				return failErr(1, err)
			}
		}
		if !plan.noRemoteWork && usesAnsible {
			reporter.BundleStart()
			bundleResult, err = prepareWorkflowBundle(false)
			if err != nil {
				return failErr(1, err)
			}
			reporter.BundleReady(bundleResult)
			runOpts.BundleDir = bundleResult.Dir
		}
		ledger, err := workflow.RunPreparedApplyTaskGraph(c.Context(), stdout, stderr, ctx.RunsDir, runOpts, applyTarget, flags.clusterScope, prepared, newApplyReporter(stdout, stderr, ctx.Name, ctx.RunsDir, clustersDir), nil)
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
		if plan.targetsClusters {
			printClusterAccess(stdout, plan.state, renderResult, ledger, clustersDir)
		}
		return nil
	}
	return cmd
}

// overrideDriftedStorageSubObjects returns the labels of storage sub-objects that
// --override would rebuild (those that drifted from their recorded desired state), in
// the classifier's stable order. It drives the data-loss warning before the confirm.
func overrideDriftedStorageSubObjects(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) && o.HasDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}
