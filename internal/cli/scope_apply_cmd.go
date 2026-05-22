package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workflow"
)

func newScopeApplyCmd(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		flags         scopeCommonFlags
		dryRun        bool
		check         bool
		askBecomePass bool
		yes           bool
		strictSecrets bool
		parallelism   int
		perHost       int
		redfish       int
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply " + scope.name + " desired state",
		Args:  cobra.NoArgs,
		Example: fmt.Sprintf(`  # Preview the plan without changing anything
  bootwright apply %[1]s --dry-run

  # Apply non-interactively (skip the confirmation prompt)
  bootwright apply %[1]s --yes

  # Apply only specific clusters
  bootwright apply %[1]s --scope managed-01 --yes

  # Apply when passwordless sudo is available on provider hosts
  bootwright apply %[1]s --ask-become-pass=false --yes`, scope.name),
	}
	cf := addCommonFlags(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible commands without executing them")
	cmd.Flags().BoolVar(&check, "check", false, "pass --check to ansible-playbook")
	cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the Ansible become password; defaults to false when bootwright runs as root, true otherwise")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the apply confirmation prompt")
	cmd.Flags().BoolVar(&strictSecrets, "strict-secrets", false, "abort if context secrets-dir mode is not 0700 or any secret file mode is not 0600 (default: warn only)")
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "maximum concurrent apply tasks (0 auto)")
	cmd.Flags().IntVar(&perHost, "parallelism-per-host", 4, "maximum concurrent apply tasks per provider host")
	cmd.Flags().IntVar(&redfish, "parallelism-redfish", 8, "maximum concurrent Redfish boot tasks")
	registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, false), "apply")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		if strictSecrets {
			if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
				return e
			}
		}
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(scope.name + " apply")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + scope.name + " apply"}})
		}
		if err := validateScopedApplySharedServices(state, scope.name, flags.clusterScope); err != nil {
			return failErr(1, err)
		}
		plan, err := prepareScopedWorkflow(state, scope, flags.clusterScope, flags.hostStateDir, askBecomePass, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if err := ensureApplySupported(plan.state); err != nil {
			return failErr(1, err)
		}
		limits := workflow.ConcurrencyLimits{
			Parallelism:        parallelism,
			ParallelismPerHost: perHost,
			ParallelismRedfish: redfish,
		}
		tasks := planApplyTasks(scope, plan.state)
		if limits.Parallelism <= 0 {
			limits.Parallelism = autoParallelism(len(tasks))
		}
		if plan.askBecomePass && limits.Parallelism > 1 {
			limits.Parallelism = 1
		}
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped apply commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, "apply", plan.state, plan.selected, scope.applyPlaybook, plan.limit, plan.extraVarPairs, scope.artifactsBaseName, check, plan.askBecomePass, plan.targetsClusters, limits, tasks)
		}
		if !dryRun {
			existing, ok, err := workflow.LoadRunLedger(ctx.StateDir)
			if err != nil {
				return failErr(1, err)
			}
			if ok && existing.Active() {
				return failf(1, "apply run %s is still running; inspect it with bootwright status --watch", existing.RunID)
			}
			if err := runApplyHostCheck(stdout, stderr, plan.state, plan.selected, ctx.SecretsDir, flags.hostStateDir); err != nil {
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
		reporter := newWorkflowReporter(stdout)
		if plan.askBecomePass {
			reporter.WithPromptGap(stderr)
		}
		if !dryRun && !plan.noRemoteWork {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(ctx.StateDir, dryRun || plan.noRemoteWork)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.noRemoteWork {
			reporter.BundleReady(bundle)
		}
		runOpts := workflow.RunOptions{
			State:             plan.state,
			StateDir:          ctx.StateDir,
			SecretsDir:        ctx.SecretsDir,
			HostStateDir:      flags.hostStateDir,
			Executable:        flags.executable,
			BundleDir:         bundle.Dir,
			Playbook:          scope.applyPlaybook,
			Limit:             plan.limit,
			ExtraVarPairs:     plan.extraVarPairs,
			ArtifactsBaseName: scope.artifactsBaseName,
			Check:             check,
			AskBecomePass:     plan.askBecomePass,
			UseControllingTTY: useControllingTTYForWorkflow(plan.selected, plan.askBecomePass),
			DryRun:            dryRun,
			ResolveInstaller:  plan.targetsClusters,
			Label:             scope.name + " apply",
		}
		if dryRun {
			reporter.DryRunTasks(scope.name+" apply", taskLedgerEntries(tasks), limits)
			result, err := workflow.RenderOnly(ctx.StateDir, ctx.SecretsDir, plan.state)
			if err != nil {
				return failErr(1, err)
			}
			printRenderResult(stdout, result)
			printBundlePath(stdout, bundle.Dir)
			return nil
		}
		if reporter != nil {
			reporter.RenderStart()
		}
		renderResult, err := workflow.RenderOnly(ctx.StateDir, ctx.SecretsDir, plan.state)
		if err != nil {
			return failErr(1, err)
		}
		if plan.targetsClusters {
			reporter.ResolveInstallerStart()
			if _, err := workflow.ResolveInstaller(ctx.StateDir, ctx.SecretsDir, plan.state); err != nil {
				return failErr(1, err)
			}
		}
		ledger, err := runApplyTaskGraph(c.Context(), stdout, stderr, ctx.StateDir, runOpts, scope, flags.clusterScope, tasks, limits)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowEnd(stdout, scope.name+" apply")
		}
		printRenderResult(stdout, renderResult)
		printBundlePath(stdout, bundle.Dir)
		if plan.targetsClusters {
			printClusterAccess(stdout, plan.state, renderResult, ledger)
		}
		return nil
	}
	return cmd
}
