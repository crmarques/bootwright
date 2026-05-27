package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/workflow"
)

func newCheckAllCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		executable string
		dryRun     bool
	)
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Check all provisioning prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Run bastion + infra + cluster checks against the current context
  bootwright check all

  # Print the planned Ansible preflight command without executing
  bootwright check all --dry-run`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&executable, "ansible-playbook", resolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		outputpkg(stdout).Command("all check")
		if err := runBastionChecks(stdout); err != nil {
			return err
		}
		ctx := cf.ctx
		runtimeDir := controllerRuntimeDir(ctx.Name)
		if err := runScopeHostCheck(stdout, stderr, state, allScope.phases(), ctx.SecretsDir, ctx.ManagedDir); err != nil {
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
			Executable:        executable,
			BundleDir:         bundle.Dir,
			Playbook:          "playbooks/checks/preflight.yml",
			ArtifactsBaseName: "preflight-all",
			DryRun:            dryRun,
			Label:             "all check",
		}, runner, reporter)
		if err != nil {
			return failErr(1, err)
		}
		return nil
	}
	return cmd
}
