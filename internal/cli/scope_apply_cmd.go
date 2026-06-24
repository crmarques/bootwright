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
	"github.com/crmarques/bootwright/internal/workspace"
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

func newScopeApplyCmdWithOptions(scope converge.Scope, stdin io.Reader, stdout io.Writer, stderr io.Writer, options scopeApplyOptions) *cobra.Command {
	usesAnsible := converge.ScopeUsesAnsible(scope)
	var (
		flags            scopeCommonFlags
		dryRun           = options.defaultPlan
		check            bool
		askBecomePass    bool
		yes              bool
		strictSecrets    bool
		override         bool
		expectNew        bool
		trustOnFirstUse  bool
		parallelism      int
		perHost          int
		redfish          int
		stage            string
		streamAnsible    bool
		scopedValidation bool
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
		cmd.Flags().BoolVar(&streamAnsible, "stream-ansible", false, "stream raw ansible-playbook output to the terminal as well as the run log (default: log only); the progress view falls back to plain transition lines")
	}
	if !options.hideApproval {
		cmd.Flags().BoolVar(&yes, "yes", false, "skip the apply confirmation prompt")
		cmd.Flags().BoolVar(&trustOnFirstUse, "trust-on-first-use", true, "prompt to record an unknown SSH host key after showing its fingerprint (interactive text runs only; never under --yes or --output json); automation must pre-record trust with bootwright host trust")
	}
	cmd.Flags().BoolVar(&strictSecrets, "strict-secrets", false, "abort if context secrets-dir mode is not 0700 or any secret file mode is not 0600 (default: warn only)")
	cmd.Flags().BoolVar(&expectNew, "expect-new", false, "assert a greenfield run: fail if any selected object already exists; without it apply reconciles (creates what is missing, skips what matches, fails closed on drift)")
	if converge.ScopeTargetsContainerInstall(scope) {
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
			cmd.Flags().StringVar(&flags.executable, "ansible-playbook", workspace.ResolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
		}
		cmd.Flags().StringVar(&flags.output, "output", flags.output, "output format: text|json (json is supported with --dry-run)")
		cmd.Flags().StringVar(&stage, "stage", "", fmt.Sprintf("stage to %s: %s families, or a sub-phase %s (default full graph)", action, strings.Join(converge.FamilyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
		registerStageCompletion(cmd, converge.ApplyStageNames())
		cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to apply")
		cmd.Flags().BoolVar(&scopedValidation, "scoped-validation", false, "validate only the resources within the selected --clusters/--stage scope, ignoring desired-state errors in objects outside it (no effect without --clusters)")
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
			runScope, err = converge.ApplyStageScope(stage)
			if err != nil {
				return failErr(2, err)
			}
			runCommandLabel = converge.ApplyStageCommandLabel(stage, action, commandLabel)
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
		if scopedValidation {
			state, err = loadDesiredStateScopedValidation(cf, runScope.Name, flags.clusterScope)
		} else {
			state, err = loadDesiredState(cf)
		}
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + runCommandLabel}})
		}
		storageSelectionActive := false
		if options.stageSelector && strings.TrimSpace(flags.clusterScope) != "" {
			// Validate the requested names here; the effective storage selection is
			// derived from the scoped plan.State below so it includes
			// Data Foundation attachment targets pulled in transitively.
			if _, _, err := clusteraccess.ClusterRootNamesForTarget(state, flags.clusterScope); err != nil {
				return failErr(1, err)
			}
			storageSelectionActive = true
		}
		if err := clusteraccess.ValidateScopedApplySharedServices(state, runScope.Name, flags.clusterScope); err != nil {
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
		if converge.ScopeUsesAnsible(runScope) {
			if err := workflow.EnsureApplySupported(plan.State); err != nil {
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
				if err := converge.CheckApplyOverrideDestroyProtection(plan.State); err != nil {
					return failErr(1, err)
				}
			}
		}
		applyTarget, tasks, limits, dryRunTasks, err := converge.PlanScopedApply(runScope, &plan, mode, storageSelectionActive, workflow.ConcurrencyLimits{
			Parallelism:        parallelism,
			ParallelismPerHost: perHost,
			ParallelismRedfish: redfish,
		}, clustersDir)
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, runScope, action, plan.State, plan.Selected, runScope.ApplyPlaybook, plan.Limit, plan.ExtraVarPairs, runScope.ArtifactsBaseName, check, plan.AskBecomePass, plan.TargetsClusters, limits, dryRunTasks, nil, workflow.AnsibleForksForLimit(plan.State, plan.Limit))
		}
		if !dryRun {
			objects, err := converge.ApplyModePreflight(mode, tasks, ctx.RunsDir)
			if err != nil {
				return failErr(1, err)
			}
			// --override rebuilds drifted storage sub-objects; a structural change
			// (pool type, CephFS metadata pool) is data-destroying. Warn before the
			// confirm prompt so the operator sees which pools/filesystems are at risk.
			if mode == workflow.ApplyModeOverride {
				if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
					cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type or CephFS metadata pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
				}
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
			if err := runApplyHostCheck(stdout, stderr, plan.State, plan.Selected, ctx.Name, ctx.SecretsDir, clustersDir, hostTrustScope); err != nil {
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
		runOpts := converge.BuildApplyRunOptions(ctx, clustersDir, flags.executable, runScope, plan, check, become.PasswordFile, dryRun, runCommandLabel, mode, streamAnsible)
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
		renderResult, bundleResult, ledger, err := converge.ExecuteApply(c.Context(), stdout, stderr, ctx, clustersDir, runOpts, applyTarget, flags.clusterScope, plan, tasks, limits, usesAnsible, bundleResult, bundleVersionMarker(), reporter, newApplyReporter(stdout, stderr, ctx.Name, ctx.RunsDir, clustersDir, buildClusterDisplays(state), streamAnsible))
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
