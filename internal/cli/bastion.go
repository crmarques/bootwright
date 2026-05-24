package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
)

func newBastionCheckCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	hostStateDir := defaultHostStateDir
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check controller dependencies",
		Args:  cobra.NoArgs,
		Example: `  # Check the bastion has the runtime + CLIs the current context needs
  bootwright check bastion`,
	}
	cf := addCommonFlags(cmd)
	cmd.Flags().StringVar(&hostStateDir, "host-state-dir", hostStateDir, "root-managed host runtime state directory")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		output.New(stdout).Command("bastion check")
		return runBastionChecks(stdout, stderr, state, hostStateDir)
	}
	return cmd
}

func runBastionChecks(stdout io.Writer, _ io.Writer, state v1alpha1.State, hostStateDir string) error {
	checks := collectBastionChecks(state, hostStateDir, defaultPreflightDeps)
	return renderCheckResults(stdout, "bastion check", checks)
}

func newBastionApplyCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		dryRun        bool
		yes           bool
		askBecomePass bool
		strictSecrets bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Install controller prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Install runtime and release-specific OCP CLIs for the current context
  bootwright apply bastion --yes

  # Show planned actions without changing the host
  bootwright apply bastion --dry-run

  # Apply non-interactively when passwordless sudo is available
  bootwright apply bastion --ask-become-pass=false --yes`,
	}
	cf := addCommonFlags(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned commands without executing them")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the sudo password when controller CLI install needs root; defaults to false when bootwright runs as root, true otherwise")
	cmd.Flags().BoolVar(&strictSecrets, "strict-secrets", false, "abort if context secrets-dir mode is not 0700 or any secret file mode is not 0600 (default: warn only)")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		if strictSecrets {
			if e := strictSecretsDirCheck(ctx.SecretsDir); e != nil {
				return e
			}
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		proxyEnv, err := resolveProxyEnv(state, ctx.SecretsDir)
		if err != nil {
			return failErr(1, err)
		}
		plan, err := controllerBootstrapPlan(len(proxyEnv) > 0)
		if err != nil {
			return failErr(1, err)
		}
		cliSpec := planControllerCLIInstall(state, defaultControllerCLIInstallDir())

		p := output.New(stdout)
		p.Command("bastion apply")
		p.Section("Plan")
		fields := []output.Field{{Key: "ansible-core target", Value: "managed venv at " + ansibleVenvDir()}}
		if summary := proxySummary(proxyEnv); summary != "" {
			fields = append(fields, output.Field{Key: "proxy", Value: summary})
		} else {
			fields = append(fields, output.Field{Key: "proxy", Value: "none"})
		}
		p.Fields(fields)
		p.Section("Bootwright prerequisites")
		if len(plan) == 0 {
			p.Status(output.StatusSkip, "controller runtime", "already installed")
		}
		for _, step := range plan {
			p.CommandLine(step.Label, step.Cmd)
		}
		switch {
		case cliSpec != nil:
			cliInstallEnv := controllerCLIAnsibleEnv(cliSpec.BundleDir)
			cliInstallCommand := controllerCLIInstallCommand(cliSpec.PlannedCommand(controllerCLILocalInventory), cliInstallEnv, askBecomePass)
			p.CommandLine("install OCP CLIs "+cliSpec.OCPReleaseVersion+" into "+cliSpec.InstallDir, cliInstallCommand)
		default:
			p.Status(output.StatusSkip, "install OCP CLIs", "no openshift.release.version declared in state")
		}
		if dryRun {
			return nil
		}
		if !yes && !confirm(stdin, stdout, "Continue with bootstrap? [y/N] (default: no): ") {
			return failErr(1, errors.New("bootstrap aborted"))
		}
		if err := runBootstrapPlan(c.Context(), stdin, stdout, stderr, plan, proxyEnv); err != nil {
			return err
		}
		if cliSpec != nil {
			if err := runControllerCLIInstall(c.Context(), stdin, stdout, stderr, *cliSpec, proxyEnv, askBecomePass); err != nil {
				return failErr(1, err)
			}
		}
		output.NewContinuation(stdout).Summary(output.StatusOK, "bastion", "ready")
		return nil
	}
	return cmd
}
