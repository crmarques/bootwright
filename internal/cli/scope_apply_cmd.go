package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newScopeApplyCmd(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(scope, stdin, stdout, stderr, scopeApplyOptions{})
}

type scopeApplyOptions struct {
	use          string
	short        string
	example      string
	defaultPlan  bool
	hideDryRun   bool
	hideApproval bool
	commandLabel string
	action       string
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
		parallelism   int
		perHost       int
		redfish       int
	)
	use := "apply"
	if options.use != "" {
		use = options.use
	}
	short := "Apply " + scope.name + " desired state"
	if options.short != "" {
		short = options.short
	}
	example := scopeApplyExample(scope.name, usesAnsible)
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
	if scopeTargetsContainerInstall(scope) {
		cmd.Flags().BoolVar(&override, "override", false, "run the cluster install even when prior cluster state reports an existing available cluster")
	}
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "maximum concurrent apply tasks (0 auto safe maximum)")
	if usesAnsible {
		cmd.Flags().IntVar(&perHost, "parallelism-per-host", 0, "maximum concurrent mutating tasks per provider host (0 auto safe maximum)")
		cmd.Flags().IntVar(&redfish, "parallelism-redfish", 0, "maximum concurrent Redfish boot tasks (0 auto safe maximum)")
	}
	registerScopeCommonFlagsWithAnsibleTarget(cmd, &flags, scopeAllowsClusterScope(scope, false), action, usesAnsible, scopeTargetKind(scope))
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
			p.Command(commandLabel)
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + commandLabel}})
		}
		if err := validateScopedApplySharedServices(state, scope.name, flags.clusterScope); err != nil {
			return failErr(1, err)
		}
		plan, err := prepareScopedApplyWorkflow(state, scope, flags.clusterScope, askBecomePass, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if scopeUsesAnsible(scope) {
			if err := workflow.EnsureApplySupported(plan.state); err != nil {
				return failErr(1, err)
			}
		}
		if override {
			plan.extraVarPairs = append(plan.extraVarPairs, "bootwright_install_override=true")
		}
		limits := workflow.ConcurrencyLimits{
			Parallelism:        parallelism,
			ParallelismPerHost: perHost,
			ParallelismRedfish: redfish,
		}
		applyTarget := scope.applyTarget()
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
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, action, plan.state, plan.selected, scope.applyPlaybook, plan.limit, plan.extraVarPairs, scope.artifactsBaseName, check, plan.askBecomePass, plan.targetsClusters, limits, dryRunTasks, workflow.AnsibleForksForLimit(plan.state, plan.limit))
		}
		if !dryRun {
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
			printWorkflowStart(stdout, scope.name, plan.selected, plan.askBecomePass)
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
		usesAnsible := scopeUsesAnsible(scope)
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
			SecretsDir:         ctx.SecretsDir,
			ManagedServicesDir: ctx.ManagedServicesDir,
			ProviderStateDir:   ctx.ProviderStateDir,
			Executable:         flags.executable,
			Playbook:           scope.applyPlaybook,
			Limit:              plan.limit,
			Forks:              workflow.AnsibleForksForLimit(plan.state, plan.limit),
			ExtraVarPairs:      plan.extraVarPairs,
			ArtifactsBaseName:  scope.artifactsBaseName,
			Check:              check,
			AskBecomePass:      plan.askBecomePass && become.PasswordFile == "",
			BecomePasswordFile: become.PasswordFile,
			UseControllingTTY:  useControllingTTYForWorkflow(plan.selected, plan.askBecomePass && become.PasswordFile == ""),
			DryRun:             dryRun,
			ResolveInstaller:   plan.targetsClusters,
			Label:              commandLabel,
			InstallOverride:    override,
		}
		if dryRun {
			cliout.NewContinuation(stdout).Warning("dry-run", "plan only; run bootwright check "+scope.name+" to validate secrets, tools, and remote readiness")
			reporter.DryRunTasks(commandLabel, workflow.TaskLedgerEntries(dryRunTasks), limits)
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
		if plan.targetsClusters {
			reporter.ResolveInstallerStart()
			if _, err := workflow.ResolveInstaller(clustersDir, ctx.SecretsDir, plan.state); err != nil {
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
			printClusterAccess(stdout, plan.state, renderResult, ledger)
		}
		return nil
	}
	return cmd
}

func scopeApplyExample(scopeName string, usesAnsible bool) string {
	if !usesAnsible {
		return fmt.Sprintf(`  # Preview the plan only; readiness checks are not run
  bootwright apply %[1]s --dry-run

  # Apply non-interactively (skip the confirmation prompt)
  bootwright apply %[1]s --yes

  # Apply only specific clusters
  bootwright apply %[1]s --scope managed-01 --yes`, scopeName)
	}
	return fmt.Sprintf(`  # Preview the plan only; readiness checks are not run
  bootwright apply %[1]s --dry-run

  # Apply non-interactively (skip the confirmation prompt)
  bootwright apply %[1]s --yes

  # Apply only specific clusters
  bootwright apply %[1]s --scope managed-01 --yes

  # Apply when passwordless sudo is available on provider hosts
  bootwright apply %[1]s --ask-become-pass=false --yes`, scopeName)
}

func scopeUsesAnsible(scope scopeSpec) bool {
	return scope.name != "addons" && scope.name != "storage-cluster"
}

func scopeTargetsContainerInstall(scope scopeSpec) bool {
	switch scope.name {
	case "clusters", "container-cluster", "all":
		return true
	default:
		return false
	}
}
