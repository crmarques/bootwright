package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newScopeCheckCmd(scope scopeSpec, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		flags  scopeCommonFlags
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check " + scope.name + " prerequisites",
		Args:  cobra.NoArgs,
		Example: fmt.Sprintf(`  # Validate the current context and run the read-only Ansible preflight
  bootwright check %[1]s

  # Limit to specific clusters
  bootwright check %[1]s --scope sno-libvirt,managed-01

  # Print the planned Ansible command without executing it
  bootwright check %[1]s --dry-run`, scope.name),
	}
	cf := addCommonFlags()
	registerScopeCommonFlags(cmd, &flags, scopeAllowsClusterScope(scope, false), "check")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(flags.output); err != nil {
			return failErr(2, err)
		}
		if flags.output == outputText {
			p := cliout.New(stdout)
			p.Command(scope.name + " check")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		runtimeDir := controllerRuntimeDir(ctx.Name)
		if flags.output == outputText {
			cliout.New(stdout).List([]cliout.Item{{Label: "Plan " + scope.name + " check"}})
		}
		state, err = scopeState(state, scope.name, flags.clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		limit := ansibleLimitForScope(scope.name)
		if flags.output == outputJSON {
			if !dryRun {
				return failErr(2, errors.New("--output json is supported with --dry-run for scoped check commands"))
			}
			selected := phasesForState(scope.phases(), state)
			return runScopeDryRunJSON(c, stdout, cf, flags, scope, "check", state, selected, preflightPlaybookPath, limit, nil, "preflight-"+scope.name, false, false, false, workflow.ConcurrencyLimits{}, nil, 0)
		}
		if err := runScopeHostCheck(stdout, stderr, state, scope.phases(), ctx.SecretsDir, runtimeDir); err != nil {
			return err
		}
		reporter := newWorkflowReporter(stdout)
		if !dryRun {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun {
			reporter.BundleReady(bundle)
		}
		runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		_, err = workflow.Run(c.Context(), workflow.RunOptions{
			State:             state,
			RenderedDir:       ctx.RenderedDir,
			RuntimeDir:        runtimeDir,
			RunsDir:           ctx.RunsDir,
			SecretsDir:        ctx.SecretsDir,
			ManagedDir:        ctx.ManagedDir,
			Executable:        flags.executable,
			BundleDir:         bundle.Dir,
			Playbook:          preflightPlaybookPath,
			Limit:             limit,
			ArtifactsBaseName: "preflight-" + scope.name,
			DryRun:            dryRun,
			Label:             scope.name + " check",
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
		}
		return nil
	}
	return cmd
}
