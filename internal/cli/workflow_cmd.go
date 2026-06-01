package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

func renderOutputDirRequiresSensitiveError(outputDir string) error {
	return fmt.Errorf("render --output-dir %s would write OpenShift installer files with secret material\nrerun with --sensitive only for a local, unversioned directory\nprotect those files while they exist and remove them when no longer needed", outputDir)
}

func newCheckCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <target>",
		Short: "Validate desired state and prerequisites",
	}
	cmd.AddCommand(
		newCheckSyntaxCmd(stdout),
		retargetCommand(newBastionCheckCmd(stdout), "bastion", "Verify bastion dependencies"),
		retargetCommand(newScopeCheckCmd(infraScope, stdout, stderr), "infra", "Check infrastructure hosts and substrate"),
		retargetCommand(newScopeCheckCmd(clustersScope, stdout, stderr), "clusters", "Check cluster lifecycle prerequisites"),
		retargetCommand(newScopeCheckCmd(containerClusterScope, stdout, stderr), "container-cluster", "Check container cluster install prerequisites"),
		retargetCommand(newScopeCheckCmd(storageClusterScope, stdout, stderr), "storage-cluster", "Check storage cluster prerequisites"),
		retargetCommand(newAddonsCheckCmd(stdout), "addons", "Check post-install cluster addon prerequisites"),
		newCheckAllCmd(stdout, stderr),
	)
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newApplyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <target>",
		Short: "Apply a provisioning target",
	}
	cmd.AddCommand(
		retargetCommand(newBastionApplyCmd(stdin, stdout, stderr), "bastion", "Install bastion prerequisites"),
		retargetCommand(newScopeApplyCmd(infraScope, stdin, stdout, stderr), "infra", "Converge infrastructure hosts and substrate"),
		retargetCommand(newScopeApplyCmd(clustersScope, stdin, stdout, stderr), "clusters", "Provision cluster infrastructure, storage, OpenShift clusters, addons, and integrations"),
		retargetCommand(newScopeApplyCmd(containerClusterScope, stdin, stdout, stderr), "container-cluster", "Install OpenShift clusters and apply addons"),
		retargetCommand(newScopeApplyCmd(storageClusterScope, stdin, stdout, stderr), "storage-cluster", "Provision external storage clusters"),
		retargetCommand(newScopeApplyCmd(addonsScope, stdin, stdout, stderr), "addons", "Apply post-install cluster addons"),
		retargetCommand(newScopeApplyCmd(allScope, stdin, stdout, stderr), "all", "Apply infrastructure, storage, OpenShift clusters, and addons"),
	)
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newPlanCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(allScope, stdin, stdout, stderr, scopeApplyOptions{
		use:          "plan",
		short:        "Preview the complete provisioning plan",
		defaultPlan:  true,
		hideDryRun:   true,
		hideApproval: true,
		commandLabel: "plan",
		action:       "plan",
		example: `  # Preview the full provisioning plan
  bootwright plan

  # Preview only selected managed clusters
  bootwright plan --scope managed-01

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
  bootwright render --output-dir ./rendered --scope managed-01 --sensitive

  # Inspect normalized desired state with defaults applied
  bootwright render effective

  # Render placeholder installer files into the context rendered-dir
  bootwright render installer`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "write concrete tool input files to this directory")
	cmd.Flags().StringVar(&clusterScope, "scope", "", "comma-separated ContainerCluster names to render with --output-dir")
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
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newDestroyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy <target>",
		Short: "Tear down a previously applied target",
	}
	cmd.AddCommand(
		retargetCommand(newScopeDestroyCmd(infraScope, stdin, stdout, stderr), "infra", "Tear down infrastructure hosts and substrate"),
		retargetCommand(newScopeDestroyCmd(containerClusterScope, stdin, stdout, stderr), "container-cluster", "Tear down OpenShift cluster install state"),
	)
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
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
