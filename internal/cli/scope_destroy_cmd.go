package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/ansible"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/stategraph"
	"github.com/crmarques/bootwright/internal/workflow"
)

func newScopeDestroyCmd(scope scopeSpec, stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		flags         scopeCommonFlags
		dryRun        bool
		check         bool
		askBecomePass bool
		yes           bool
	)
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy " + scope.name + " runtime state",
		Args:  cobra.NoArgs,
		Example: fmt.Sprintf(`  # Preview what would be destroyed
  bootwright destroy %[1]s --dry-run

  # Destroy non-interactively
  bootwright destroy %[1]s --yes

  # Destroy only specific clusters
  bootwright destroy %[1]s --scope managed-01 --yes`, scope.name),
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible commands without executing them")
	cmd.Flags().BoolVar(&check, "check", false, "pass --check to ansible-playbook")
	cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the Ansible become password; defaults to false when bootwright runs as root, true otherwise")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the destroy confirmation prompt")
	registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, true), "destroy")
	if scope.name == "infra" {
		if f := cmd.Flags().Lookup("scope"); f != nil {
			f.Usage = "comma-separated ContainerCluster names to destroy, or http-server to remove only the BMC ISO HTTP service"
		}
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		runtimeDir := controllerRuntimeDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(scope.name + " destroy")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		httpServerOnly := isInfraHTTPServerDestroyScope(scope, flags.clusterScope)
		// For `destroy infra --scope`, refuse to proceed when scoped
		// clusters share a provider service component with unscoped
		// clusters: the renderer keys container names and state dirs
		// per (provider, name), so destroying a shared instance breaks
		// the unscoped consumers silently.
		if scope.name == "infra" && strings.TrimSpace(flags.clusterScope) != "" && !httpServerOnly {
			selectedNames, err := clusterNamesForTarget(state, flags.clusterScope)
			if err != nil {
				return failErr(1, err)
			}
			if conflicts := stategraph.SharedDestroyConflicts(state, selectedNames); len(conflicts) > 0 {
				return failErr(1, formatDestroyScopeConflicts(conflicts))
			}
		}
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + scope.name + " destroy"}})
		}
		playbook := scope.destroyPlaybook
		artifactsBaseName := scope.artifactsBaseName + "-destroy"
		workflowLabel := scope.name + " destroy"
		var plan scopedWorkflowPlan
		if httpServerOnly {
			plan = prepareInfraHTTPServerDestroyWorkflow(state, askBecomePass, dryRun)
			playbook = infraDestroyHTTPServerPlaybook
			artifactsBaseName = infraDestroyHTTPServerArtifactsBaseName
			workflowLabel = "infra destroy http-server"
		} else {
			plan, err = prepareScopedWorkflow(state, scope, flags.clusterScope, ctx.BaseDir, askBecomePass, dryRun)
			if err != nil {
				return failErr(1, err)
			}
		}
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped destroy commands"))
			}
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, "destroy", plan.state, plan.selected, playbook, plan.limit, plan.extraVarPairs, artifactsBaseName, check, plan.askBecomePass, false, workflow.ConcurrencyLimits{}, nil, 0)
		}
		if !dryRun {
			if err := reconcileCurrentApplyBeforeMutation(stdout, ctx.StateDir); err != nil {
				return failErr(1, err)
			}
		}
		if httpServerOnly {
			printDestroyHTTPServerPreview(stdout, plan.state)
		} else {
			printDestroyPreview(stdout, scope, runtimeDir, plan.state)
		}
		printDestroySummary(stdout, plan.selected, plan.askBecomePass, dryRun, plan.noRemoteWork)
		if !dryRun && !yes && !plan.noRemoteWork {
			if !confirm(stdin, stdout, "Continue with destroy? [y/N] (default: no): ") {
				return failErr(1, errors.New("destroy aborted"))
			}
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowStart(stdout, workflowLabel, plan.selected, plan.askBecomePass)
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
		runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		runResult, err := workflow.Run(c.Context(), workflow.RunOptions{
			State:              plan.state,
			StateDir:           ctx.StateDir,
			RuntimeDir:         runtimeDir,
			SecretsDir:         ctx.SecretsDir,
			HostStateDir:       ctx.BaseDir,
			Executable:         flags.executable,
			BundleDir:          bundle.Dir,
			Playbook:           playbook,
			Limit:              plan.limit,
			ExtraVarPairs:      plan.extraVarPairs,
			ArtifactsBaseName:  artifactsBaseName,
			Check:              check,
			AskBecomePass:      plan.askBecomePass && become.PasswordFile == "",
			BecomePasswordFile: become.PasswordFile,
			UseControllingTTY:  useControllingTTYForWorkflow(plan.selected, plan.askBecomePass && become.PasswordFile == ""),
			DryRun:             dryRun,
			Label:              workflowLabel,
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun && !plan.noRemoteWork {
			printWorkflowEnd(stdout, workflowLabel)
		}
		printRenderResult(stdout, runResult.Render)
		printBundlePath(stdout, bundle.Dir)
		return nil
	}
	return cmd
}
