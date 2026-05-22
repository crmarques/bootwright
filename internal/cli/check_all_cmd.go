package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/workflow"
)

func newCheckAllCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		executable   string
		hostStateDir string
		dryRun       bool
	)
	hostStateDir = defaultHostStateDir
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Check all provisioning prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Run bastion + infra + cluster checks against the current context
  bootwright check all

  # Print the planned Ansible preflight command without executing
  bootwright check all --dry-run`,
	}
	cf := addCommonFlags(cmd)
	cmd.Flags().StringVar(&executable, "ansible-playbook", resolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
	cmd.Flags().StringVar(&hostStateDir, "host-state-dir", hostStateDir, "root-managed host runtime state directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render artifacts and print the Ansible preflight command without executing it")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		outputpkg(stdout).Command("all check")
		if err := runBastionChecks(stdout, stderr, state, hostStateDir); err != nil {
			return err
		}
		ctx := cf.ctx
		if err := runScopeHostCheck(stdout, stderr, state, allScope.phases(), ctx.SecretsDir, hostStateDir); err != nil {
			return err
		}
		reporter := newWorkflowReporter(stdout)
		if !dryRun {
			reporter.BundleStart()
		}
		bundle, err := prepareWorkflowBundle(ctx.StateDir, dryRun)
		if err != nil {
			return failErr(1, err)
		}
		if !dryRun {
			reporter.BundleReady(bundle)
		}
		runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		_, err = workflow.Run(c.Context(), workflow.RunOptions{
			State:             state,
			StateDir:          ctx.StateDir,
			SecretsDir:        ctx.SecretsDir,
			HostStateDir:      hostStateDir,
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
