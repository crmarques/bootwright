package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bastion"
)

func newBastionCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bastion <command>",
		Short: "Manage bastion prerequisites",
	}
	cmd.AddCommand(newBastionSetupCmd(stdin, stdout, stderr))
	requireSubcommand(cmd)
	return cmd
}

func newBastionCheckCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check bastion dependencies",
		Args:  cobra.NoArgs,
		Example: `  # Check the bastion has the runtime + CLIs the current context needs
  bootwright check bastion`,
	}
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		_, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		output.New(stdout).Command("bastion check")
		return runBastionChecks(stdout)
	}
	return cmd
}

func runBastionChecks(stdout io.Writer) error {
	checks := collectBastionChecks(defaultPreflightDeps)
	return renderCheckResults(stdout, "bastion check", checks)
}

func newBastionSetupCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var (
		dryRun        bool
		yes           bool
		askBecomePass bool
		strictSecrets bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install bastion prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Install runtime and release-specific OCP CLIs for the current context
  bootwright bastion setup --yes

  # Show planned actions without changing the host
  bootwright bastion setup --dry-run

  # Apply non-interactively when passwordless sudo is available
  bootwright bastion setup --ask-become-pass=false --yes`,
	}
	cf := addCommonFlags()
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned actions without executing them")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&askBecomePass, "ask-become-pass", askBecomePassDefault(), "prompt for the Ansible become password; defaults to false when bootwright runs as root, true otherwise")
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
		proxyEnv, err := resolveProxyEnvForContext(ctx.Name, state, ctx.SecretsDir)
		if err != nil {
			return failErr(1, err)
		}
		plan, err := controllerBootstrapPlan(len(proxyEnv) > 0)
		if err != nil {
			return failErr(1, err)
		}
		cliSpec := planControllerCLIInstall(state, defaultControllerCLIInstallDir())

		p := output.New(stdout)
		p.Command("bastion setup")
		p.Section("Plan")
		fields := []output.Field{{Key: "runtime", Value: "managed Ansible environment"}}
		if cliSpec != nil {
			fields = append(fields, output.Field{Key: "OpenShift CLIs", Value: cliSpec.OCPReleaseVersion + " into " + cliSpec.InstallDir})
		} else {
			fields = append(fields, output.Field{Key: "OpenShift CLIs", Value: "not required"})
		}
		if summary := localRootSudoSummary(); summary != "" {
			fields = append(fields, output.Field{Key: "sudo", Value: summary})
		}
		if summary := proxySummary(proxyEnv); summary != "" {
			fields = append(fields, output.Field{Key: "proxy", Value: summary})
		} else {
			fields = append(fields, output.Field{Key: "proxy", Value: "none"})
		}
		p.Fields(fields)
		p.Section("Actions")
		if len(plan) == 0 {
			p.Status(output.StatusSkip, "Ansible runtime", "already installed")
		} else {
			p.Status(output.StatusPending, "Ansible runtime", bootstrapPlanUserSummary(plan))
		}
		switch {
		case cliSpec != nil:
			p.Status(output.StatusPending, "OpenShift CLIs", cliSpec.OCPReleaseVersion+" into "+cliSpec.InstallDir)
		default:
			p.Status(output.StatusSkip, "OpenShift CLIs", "no openshift.release.version declared in state")
		}
		if dryRun {
			return nil
		}
		if !yes && !confirm(stdin, stdout, "Continue with bootstrap? [y/N] (default: no): ") {
			return failErr(1, errors.New("bootstrap aborted"))
		}
		becomePassword := ""
		becomePasswordFile := ""
		needsBootstrapPassword := bootstrapPlanNeedsSudo(plan)
		needsAnsiblePasswordFile := cliSpec != nil
		if askBecomePass && (needsBootstrapPassword || needsAnsiblePasswordFile) && willPromptForBecomePassword(askBecomePass) {
			output.NewContinuation(stderr).BlankLine()
		}
		if askBecomePass && (needsBootstrapPassword || needsAnsiblePasswordFile) {
			credential, cleanup, err := prepareBecomeCredential(stdin, stderr, askBecomePass, needsBootstrapPassword, needsAnsiblePasswordFile)
			if err != nil {
				return failErr(1, err)
			}
			defer cleanup()
			becomePassword = credential.Password
			becomePasswordFile = credential.PasswordFile
		}
		if len(plan) > 0 || cliSpec != nil {
			output.NewContinuation(stdout).Section("Run")
		}
		if err := runBootstrapPlan(c.Context(), stdin, stdout, stderr, plan, proxyEnv, becomePassword, askBecomePass); err != nil {
			return err
		}
		if cliSpec != nil {
			if err := runControllerCLIInstallWithBecomePasswordFile(c.Context(), stdin, stdout, stderr, state, ctx.SecretsDir, *cliSpec, proxyEnv, askBecomePass, becomePasswordFile); err != nil {
				return failErr(1, err)
			}
		}
		output.NewContinuation(stdout).Summary(output.StatusOK, "bastion", "ready")
		return nil
	}
	return cmd
}

func bootstrapPlanUserSummary(plan []bastion.BootstrapStep) string {
	needsPython := false
	needsRuntime := false
	for _, step := range plan {
		label := strings.ToLower(step.Label)
		if strings.Contains(label, "install python") {
			needsPython = true
			continue
		}
		needsRuntime = true
	}
	var parts []string
	if needsPython {
		parts = append(parts, "install Python 3.12+")
	}
	if needsRuntime {
		parts = append(parts, "prepare managed Ansible")
	}
	if len(parts) == 0 {
		return "refresh local tools"
	}
	return strings.Join(parts, "; ")
}
