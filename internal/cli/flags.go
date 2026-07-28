package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

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

const (
	outputText = "text"
	outputJSON = "json"

	flagOutputUsage       = "output format (text|json)"
	flagOutputDryRunUsage = "output format (text|json); json requires --dry-run"

	flagDryRunUsage          = "render artifacts and print the plan; change nothing remote"
	flagAskBecomePassUsage   = "prompt for the Ansible become password (default: false as root, true otherwise)"
	flagTrustOnFirstUseUsage = "prompt to record an unknown SSH host key after showing its fingerprint (interactive runs only; never under --yes or --output json)"
	flagContextUsage         = "context to operate in (default: current context)"
	flagPreferredIDKeyUsage  = "SSH private key to offer first when reaching machines (for example ~/.ssh/id_ed25519); the declared spec.access.ssh credentials are still offered when it is not accepted"
	flagVerboseUsage         = "print full Ansible task output, including values normally hidden as \"censored due to no_log\" (secrets, BMC/registry/RHSM/proxy credentials, tokens, generated Ceph keys); WARNING: these are written to the terminal AND the run log"
)

func validateOutputFormat(value string) error {
	if value != outputText && value != outputJSON {
		return fmt.Errorf("--output must be %q or %q", outputText, outputJSON)
	}
	return nil
}

func addOutputFlag(cmd *cobra.Command, p *string) {
	*p = outputText
	cmd.Flags().StringVar(p, "output", outputText, flagOutputUsage)
	registerFlagCompletion(cmd, "output", []string{outputText, outputJSON})
}

func addOutputFlagDryRun(cmd *cobra.Command, p *string) {
	*p = outputText
	cmd.Flags().StringVar(p, "output", outputText, flagOutputDryRunUsage)
	registerFlagCompletion(cmd, "output", []string{outputText, outputJSON})
}

func addYesFlag(cmd *cobra.Command, p *bool, action string) {
	cmd.Flags().BoolVar(p, "yes", false, "skip the "+action+" confirmation prompt")
}

func addAskBecomePassFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "ask-become-pass", askBecomePassDefault(), flagAskBecomePassUsage)
}

func addTrustOnFirstUseFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVar(p, "trust-on-first-use", true, flagTrustOnFirstUseUsage)
}

func addVerboseFlag(cmd *cobra.Command, p *bool) {
	cmd.Flags().BoolVarP(p, "verbose", "v", false, flagVerboseUsage)
}
