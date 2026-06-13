package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

func renderOutputDirRequiresSensitiveError(outputDir string) error {
	return fmt.Errorf("render --output-dir %s would write OpenShift installer files with secret material\nrerun with --sensitive only for a local, unversioned directory\nprotect those files while they exist and remove them when no longer needed", outputDir)
}

func newPreflightCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight <target>",
		Short: "Run read-only preflight checks on hosts and prerequisites",
		Long: `Run read-only preflight checks on hosts and prerequisites.

A live preflight (without --dry-run) runs an Ansible preflight whose result is
the process exit code: 0 when every check passes, non-zero when any check
fails. Per-check pass/fail detail is in the terminal output and the run, task,
and cluster logs under Bootwright storage, not in a single result document.

--output json is accepted only with --dry-run, and returns the planned preflight
command graph (the work that would run), not pass/fail results. In CI, gate on
the live preflight exit code, and use 'preflight <target> --dry-run --output
json' to inspect the plan. For offline desired-state validation with structured
result JSON, use 'validate'.`,
	}
	cmd.AddCommand(
		retargetCommand(newBastionCheckCmd(stdout), "bastion", "Verify bastion dependencies"),
		retargetCommand(newScopeCheckCmd(converge.InfraScope, stdin, stdout, stderr), "infra", "Check infrastructure hosts and substrate"),
		retargetCommand(newScopeCheckCmd(converge.ClustersScope, stdin, stdout, stderr), "clusters", "Check cluster lifecycle prerequisites"),
		retargetCommand(newScopeCheckCmd(converge.ContainerClusterScope, stdin, stdout, stderr), "container-cluster", "Check container cluster install prerequisites"),
		retargetCommand(newScopeCheckCmd(converge.StorageClusterScope, stdin, stdout, stderr), "storage-cluster", "Check storage cluster prerequisites"),
		retargetCommand(newAddonsCheckCmd(stdout), "addons", "Check post-install cluster addon prerequisites"),
		newCheckAllCmd(stdin, stdout, stderr),
	)
	requireSubcommand(cmd)
	return cmd
}

func newApplyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeApplyOptions{
		use:           "apply",
		short:         "Apply the provisioning graph",
		stageSelector: true,
		commandLabel:  "apply",
		example: `  # Preview the full graph without readiness checks
  bootwright apply --dry-run

  # Apply the full graph non-interactively
  bootwright apply --yes

  # Prepare infrastructure for selected clusters only
  bootwright apply --stage infra --clusters dc1-ocp,dc1-child-ocp --yes

  # Install selected container and storage clusters, addons, and integrations
  bootwright apply --stage clusters --clusters dc1-ocp,ceph-storage --yes`,
	})
}

func newPlanCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeApplyOptions{
		use:           "plan",
		short:         "Preview the provisioning graph",
		defaultPlan:   true,
		hideDryRun:    true,
		hideApproval:  true,
		stageSelector: true,
		commandLabel:  "plan",
		action:        "plan",
		example: `  # Preview the full provisioning plan
  bootwright plan

  # Preview only infrastructure for selected clusters
  bootwright plan --stage infra --clusters managed-01

  # Machine-readable output for automation
  bootwright plan --output json`,
	})
}

func newRenderCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		outputDir    string
		clusterScope string
		sensitive    bool
	)
	cmd := &cobra.Command{
		Use:   "render [target]",
		Short: "Render generated artifacts",
		Args: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return cobra.OnlyValidArgs(c, args)
		},
		Example: `  # Export concrete tool input files for external execution
  bootwright render --output-dir ./rendered --sensitive

  # Render only one cluster's external tool input files
  bootwright render --output-dir ./rendered --clusters managed-01 --sensitive

  # Inspect normalized desired state with defaults applied
  bootwright render effective

  # Render placeholder installer files into the context rendered-dir
  bootwright render installer`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "write concrete tool input files to this directory")
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster and StorageCluster names to render with --output-dir")
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "allow writing secret-inlined OpenShift installer files; keep the output directory local and unversioned")
	cmd.AddCommand(
		newRenderEffectiveCmd(stdout, stderr),
		newRenderClusterInstallFilesCmd(stdout, stderr),
		newRenderStorageCmd(stdout, stderr),
	)
	for _, sub := range cmd.Commands() {
		cmd.ValidArgs = append(cmd.ValidArgs, sub.Name())
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if outputDir == "" {
			return c.Help()
		}
		if !sensitive {
			return failErr(1, renderOutputDirRequiresSensitiveError(outputDir))
		}
		return runRenderToolInputs(c, stdout, cf, outputDir, clusterScope)
	}
	return cmd
}

func newDestroyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := newScopeDestroyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeDestroyOptions{
		use:           "destroy",
		short:         "Tear down a previously applied target",
		stageSelector: true,
		commandLabel:  "destroy",
		example: `  # Tear down the whole context: clusters first, then the infra they ran on
  bootwright destroy --yes

  # Preview that full teardown without executing it
  bootwright destroy --dry-run

  # Preview infrastructure teardown for the full current context
  bootwright destroy --stage infra --dry-run

  # Destroy infrastructure for selected clusters only
  bootwright destroy --stage infra --clusters dc1-ocp,dc1-child-ocp --yes

  # Destroy a protected environment
  bootwright destroy --stage infra --override --yes

  # Tear down VMs that match the naming but lost their ownership marker
  # (e.g. machine or cluster names changed after the last apply)
  bootwright destroy --clusters ceph-storage --force-unowned --yes

  # Remove only the generated artifact publication service
  bootwright destroy --stage infra --clusters artifact-server --yes

  # Remove selected cluster-stage runtime and managed storage state
  # (--clusters implies --stage clusters)
  bootwright destroy --clusters dc1-ocp,ceph-storage --yes`,
	})
	return cmd
}

func retargetCommand(cmd *cobra.Command, use, short string) *cobra.Command {
	cmd.Use = use
	cmd.Short = short
	return cmd
}

func outputpkg(stdout io.Writer) *cliout.Printer {
	return cliout.New(stdout)
}
