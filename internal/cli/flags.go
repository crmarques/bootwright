package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/workspace"
)

// stripLeadingGlobalFlags drops a leading global persistent flag (currently
// --context) so the root gate classifies an invocation by its real
// command/subcommand regardless of where the global flag sits. The original
// args (including --context) are still forwarded verbatim to the sudo child.
func stripLeadingGlobalFlags(args []string) []string {
	for len(args) > 0 {
		switch {
		case args[0] == "--context":
			if len(args) < 2 {
				return nil
			}
			args = args[2:]
		case strings.HasPrefix(args[0], "--context="):
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

// Output format values and the single source of truth for every flag help
// string shared across commands. Per-command flags reuse these constants (or
// the add*Flag registrars below) so the same flag never drifts to a different
// wording on a different command.
const (
	outputText = "text"
	outputJSON = "json"

	flagOutputUsage       = "output format (text|json)"
	flagOutputDryRunUsage = "output format (text|json); json requires --dry-run"

	flagDryRunUsage           = "render artifacts and print the plan; change nothing remote"
	flagStreamAnsibleUsage    = "also stream raw Ansible output to the terminal (default: log only)"
	flagAskBecomePassUsage    = "prompt for the Ansible become password (default: false as root, true otherwise)"
	flagAnsiblePlaybookUsage  = "ansible-playbook executable to run (default: bootwright-managed venv)"
	flagTrustOnFirstUseUsage  = "prompt to record an unknown SSH host key after showing its fingerprint (interactive runs only; never under --yes or --output json)"
	flagStrictSecretsUsage    = "abort if the context secrets-dir is not 0700 or any secret file is not 0600 (default: warn only)"
	flagScopedValidationUsage = "validate only resources within the selected --clusters/--stage scope (no effect without --clusters)"
	flagContextUsage          = "context to operate in (default: current context)"
)

func validateOutputFormat(value string) error {
	if value != outputText && value != outputJSON {
		return fmt.Errorf("--output must be %q or %q", outputText, outputJSON)
	}
	return nil
}

// addOutputFlag registers the standard --output format flag (text|json),
// defaulting *p to text and wiring text|json shell completion. Use
// addOutputFlagDryRun on commands that only emit json under --dry-run.
func addOutputFlag(cmd *cobra.Command, p *string) {
	*p = outputText
	cmd.Flags().StringVar(p, "output", outputText, flagOutputUsage)
	registerFlagCompletion(cmd, "output", []string{outputText, outputJSON})
}

// addOutputFlagDryRun is addOutputFlag for commands whose json output is only
// produced together with --dry-run.
func addOutputFlagDryRun(cmd *cobra.Command, p *string) {
	*p = outputText
	cmd.Flags().StringVar(p, "output", outputText, flagOutputDryRunUsage)
	registerFlagCompletion(cmd, "output", []string{outputText, outputJSON})
}

// addYesFlag registers the standard --yes flag whose help names the exact
// confirmation it skips, e.g. addYesFlag(cmd, &yes, "apply") ->
// "skip the apply confirmation prompt".
func addYesFlag(cmd *cobra.Command, p *bool, action string) {
	cmd.Flags().BoolVar(p, "yes", false, "skip the "+action+" confirmation prompt")
}

// addAnsiblePlaybookFlag registers the --ansible-playbook executable override.
func addAnsiblePlaybookFlag(cmd *cobra.Command, p *string) {
	cmd.Flags().StringVar(p, "ansible-playbook", workspace.ResolveAnsiblePlaybook(), flagAnsiblePlaybookUsage)
}

// addAskBecomePassFlag registers --ask-become-pass with its root-aware default.
func addAskBecomePassFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "ask-become-pass", askBecomePassDefault(), flagAskBecomePassUsage)
}

// addStreamAnsibleFlag registers --stream-ansible.
func addStreamAnsibleFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "stream-ansible", false, flagStreamAnsibleUsage)
}

// addTrustOnFirstUseFlag registers --trust-on-first-use (default true).
func addTrustOnFirstUseFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "trust-on-first-use", true, flagTrustOnFirstUseUsage)
}
