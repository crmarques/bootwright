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
		Short: "Check hosts and prerequisites are ready to apply (live, read-only)",
		Long: `Run read-only preflight checks on hosts and prerequisites.

A live preflight (without --dry-run) runs an Ansible preflight whose result is
the process exit code: 0 when every check passes, non-zero when any check
fails. Per-check pass/fail detail is in the terminal output and the run, task,
and cluster logs under Bootwright storage.

In CI, gate on the live preflight exit code. Structured --output json behavior
varies by target: the scope-based targets (infra, clusters, container-cluster,
storage-cluster, all) accept --output json only with --dry-run, and return the
planned preflight command graph (the work that would run), not pass/fail
results; 'preflight add-ons' has no --dry-run and instead emits machine-readable
pass/fail JSON from a live run with --output json; 'preflight bastion' has
neither flag. For offline desired-state validation with structured result JSON,
use 'validate'.`,
	}
	cmd.AddCommand(
		retargetCommand(newBastionCheckCmd(stdout), "bastion", "Verify bastion dependencies"),
		retargetCommand(newScopeCheckCmd(converge.InfraScope, stdin, stdout, stderr), "infra", "Check infrastructure hosts and substrate"),
		retargetCommand(newScopeCheckCmd(converge.ClustersScope, stdin, stdout, stderr), "clusters", "Check cluster lifecycle prerequisites"),
		retargetCommand(newScopeCheckCmd(converge.ContainerClusterScope, stdin, stdout, stderr), "container-cluster", "Check container cluster install prerequisites"),
		retargetCommand(newScopeCheckCmd(converge.StorageClusterScope, stdin, stdout, stderr), "storage-cluster", "Check storage cluster prerequisites"),
		retargetCommand(newAddonsCheckCmd(stdout), "add-ons", "Check post-install cluster add-on prerequisites"),
		newCheckAllCmd(stdin, stdout, stderr),
	)
	requireSubcommand(cmd)
	return cmd
}

func newApplyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeApplyOptions{
		use:   "apply",
		short: "Apply the provisioning graph",
		long: "Applies the provisioning graph, reconciling desired state idempotently. It\n" +
			"fails closed on drift or foreign ownership before mutating; --converge-drifted\n" +
			"authorizes Bootwright-owned destructive rebuilds. Exit codes: 0 success, 2\n" +
			"usage error, non-zero on run failure.",
		stageSelector: true,
		commandLabel:  "apply",
		example: `  # Preview the full graph without readiness checks
  bootwright apply --dry-run

  # Apply the full graph non-interactively
  bootwright apply --yes

  # Prepare infrastructure for selected clusters only
  bootwright apply --stage infra --clusters dc1-ocp,dc1-child-ocp --yes

  # Install selected container and storage clusters, add-ons, and integrations
  bootwright apply --stage clusters --clusters dc1-ocp,ceph-storage --yes

  # Run everything from the beginning up to and including a stage
  bootwright apply --through base --yes

  # Run a contiguous range of stages, inclusive on both ends
  bootwright apply --stage deps --through add-ons --yes

  # Start at a stage and run through to the end of the graph
  bootwright apply --stage deps --through end --yes`,
	})
}

func newPlanCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newScopeApplyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeApplyOptions{
		use:   "plan",
		short: "Preview what apply would change (contacts nothing)",
		long: "Previews the provisioning task graph. Read-only: it contacts no hosts, BMCs,\n" +
			"or clusters, writes no runtime records, and never prompts. Exit codes: 0\n" +
			"success, 1 load error, 2 usage error.",
		defaultPlan:   true,
		hideDryRun:    true,
		hideApproval:  true,
		hideExecFlags: true,
		stageSelector: true,
		commandLabel:  "plan",
		action:        "plan",
		example: `  # Preview the full provisioning plan
  bootwright plan

  # Preview only infrastructure for selected clusters
  bootwright plan --stage infra --clusters managed-01

  # Preview everything from the beginning up to and including a stage
  bootwright plan --through base

  # Preview a contiguous range of stages, inclusive on both ends
  bootwright plan --stage deps --through end

  # Machine-readable output for automation
  bootwright plan --output json`,
	})
}

func newRenderCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		inputDir     string
		outputDir    string
		clusterScope string
		sensitive    bool
		output       string
	)
	cmd := &cobra.Command{
		Use:   "render [target]",
		Short: "Render generated artifacts",
		Long: "Renders generated artifacts from desired state. It contacts no hosts, BMCs,\n" +
			"or clusters. Writing secret-inlined installer files with --output-dir requires\n" +
			"--sensitive; --input-dir renders context-free with secrets as\n" +
			"{{ secret <name> }} placeholders.",
		Args: rejectUnknownSubcommandArgs,
		Example: `  # Export concrete tool input files for external execution
  bootwright render --output-dir ./rendered --sensitive

  # Render a portable bundle from an arbitrary input directory with no
  # configured context; secrets render as {{ secret <name> }} placeholders
  bootwright render --input-dir ./lab-input --output-dir ./rendered

  # Render only one cluster's external tool input files (ContainerCluster
  # or StorageCluster)
  bootwright render --output-dir ./rendered --clusters managed-01 --sensitive

  # Machine-readable manifest of the exported files for CI
  bootwright render --output-dir ./rendered --sensitive --output json

  # Inspect normalized desired state with defaults applied
  bootwright render effective

  # Render placeholder installer files into the context rendered-dir
  bootwright render installer`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&inputDir, "input-dir", "", "render context-free from this input directory (no configured context); secrets render as {{ secret <name> }} placeholders")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "write concrete tool input files to this directory")
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster and StorageCluster names to render with --output-dir")
	registerClusterScopeCompletion(cmd, clusterKindAny)
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "allow writing secret-inlined OpenShift installer files; keep the output directory local and unversioned")
	addOutputFlag(cmd, &output)
	cmd.AddCommand(
		newRenderEffectiveCmd(stdout, stderr),
		newRenderClusterInstallFilesCmd(stdout, stderr),
		newRenderStorageCmd(stdout, stderr),
	)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		if inputDir != "" {
			if outputDir == "" {
				return failf(2, "render --input-dir requires --output-dir")
			}
			if sensitive {
				return failf(2, "render --input-dir renders {{ secret <name> }} placeholders; --sensitive is not applicable")
			}
			if contextOverride != "" {
				return failf(2, "render --input-dir is context-free; do not combine it with --context")
			}
			return runRenderPortable(stdout, inputDir, outputDir, clusterScope, output)
		}
		if outputDir == "" {
			return c.Help()
		}
		if !sensitive {
			return failErr(1, renderOutputDirRequiresSensitiveError(outputDir))
		}
		return runRenderToolInputs(c, stdout, cf, outputDir, clusterScope, output)
	}
	return cmd
}

func newDestroyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := newScopeDestroyCmdWithOptions(converge.AllScope, stdin, stdout, stderr, scopeDestroyOptions{
		use:   "destroy",
		short: "Tear down a previously applied target",
		long: "Tears down a previously applied target using desired state and ownership\n" +
			"records. Destructive: --yes skips the confirmation prompt only, never implies\n" +
			"--force; protected contexts require --force. Exit codes: 0 success, 2\n" +
			"usage error, non-zero on teardown failure.",
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
  bootwright destroy --stage infra --force --yes

  # Tear down VMs that match the naming but lost their ownership marker
  # (e.g. machine or cluster names changed after the last apply)
  bootwright destroy --stage infra --clusters ceph-storage --include-unowned --yes

  # Recover a verified Ceph ownership marker and destroy that storage cluster
  bootwright destroy --clusters ceph-storage --recover-ceph-ownership ceph-storage=2088ddee-875b-11f1-9b98-303ea72d7724 --yes

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
