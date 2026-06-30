package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
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

	flagDryRunUsage          = "render artifacts and print the plan; change nothing remote"
	flagAskBecomePassUsage   = "prompt for the Ansible become password (default: false as root, true otherwise)"
	flagTrustOnFirstUseUsage = "prompt to record an unknown SSH host key after showing its fingerprint (interactive runs only; never under --yes or --output json)"
	flagStrictSecretsUsage   = "abort if the context secrets-dir is not 0700 or any secret file is not 0600 (default: warn only)"
	flagContextUsage         = "context to operate in (default: current context)"
	flagVerboseUsage         = "print full Ansible task output, including values normally hidden as \"censored due to no_log\" (secrets, BMC/registry/RHSM/proxy credentials, tokens, generated Ceph keys); WARNING: these are written to the terminal AND the run log"
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

// addAskBecomePassFlag registers --ask-become-pass with its root-aware default.
func addAskBecomePassFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "ask-become-pass", askBecomePassDefault(), flagAskBecomePassUsage)
}

// addTrustOnFirstUseFlag registers --trust-on-first-use (default true).
func addTrustOnFirstUseFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "trust-on-first-use", true, flagTrustOnFirstUseUsage)
}

// addVerboseFlag registers the standard --verbose/-v flag (default false), which
// surfaces task output normally redacted by no_log. The "-v" shorthand is not
// taken on the apply/destroy commands.
func addVerboseFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVarP(p, "verbose", "v", false, flagVerboseUsage)
}
