package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

var globalValueFlags = []string{"--context", "--ssh-user", "--ssh-preferred-id-key"}

var globalBoolFlags = []string{"--ssh-ask-sudo-password", "--ssh-user-for-provisioned"}

func stripLeadingGlobalFlags(args []string) []string {
	for len(args) > 0 {
		if leadingGlobalBoolFlag(args[0]) {
			args = args[1:]
			continue
		}
		flag, ok := leadingGlobalValueFlag(args[0])
		if !ok {
			return args
		}
		if args[0] == flag {
			if len(args) < 2 {
				return nil
			}
			args = args[2:]
			continue
		}
		args = args[1:]
	}
	return args
}

func leadingGlobalBoolFlag(arg string) bool {
	for _, flag := range globalBoolFlags {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func leadingGlobalValueFlag(arg string) (string, bool) {
	for _, flag := range globalValueFlags {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return flag, true
		}
	}
	return "", false
}

const (
	outputText = "text"
	outputJSON = "json"

	flagOutputUsage       = "output format (text|json)"
	flagOutputDryRunUsage = "output format (text|json); json requires --dry-run"

	flagDryRunUsage                = "render artifacts and print the plan; change nothing remote"
	flagAskBecomePassUsage         = "prompt for the Ansible become password (default: false as root, true otherwise)"
	flagTrustOnFirstUseUsage       = "prompt to record an unknown SSH host key after showing its fingerprint (interactive runs only; never under --dry-run, --yes, or --output json)"
	flagContextUsage               = "context to operate in (default: current context)"
	flagSSHPreferredIDKeyUsage     = "SSH private key to offer first when reaching machines (for example ~/.ssh/id_ed25519); the declared spec.access.ssh credentials are still offered when it is not accepted"
	flagSSHUserUsage               = "account to log in as on machines that declare spec.access.ssh.auth.operatorIdentity — the machines you already administer; a login Bootwright created or one you named a Secret for is unaffected unless --ssh-user-for-provisioned widens it, and apply/plan/destroy refuse when no selected machine uses that arm"
	flagSSHAskSudoPasswordUsage    = "prompt once, before the run starts, for the sudo password of the account --ssh-user names, and answer sudo with it on the machines that account reaches; the password is held in memory for the run only and is never written to the context secret store, the rendered inventory, or the run log"
	flagSSHUserForProvisionedUsage = "widen --ssh-user to every machine in the run, including the ones Bootwright installed and reaches as \"" + v1alpha1.BootwrightSSHUser + "\"; requires --ssh-user, and the named account must exist with sudo on those machines too, because the managed-OS ownership probe authenticates as whatever account is in force"
	flagVerboseUsage               = "print full Ansible task output, including values normally hidden as \"censored due to no_log\" (secrets, BMC/registry/RHSM/proxy credentials, tokens, generated Ceph keys); WARNING: these are written to the terminal AND the run log"
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
	cmd.Flags().BoolVar(p, "yes", false, "answer the ordinary "+action+" confirmation prompt; it never authorizes data loss or any other named risk (see --authorize)")
}

func addModeFlag(cmd *cobra.Command, p *string, action string) {
	cmd.Flags().StringVar(p, "mode", string(workflow.ApplyModeReconcile), "intent of this "+action+" ("+strings.Join(workflow.ApplyModeNames(), "|")+"): create asserts a greenfield run and fails if any selected object already exists; reconcile creates what is missing, skips what matches, and fails closed on drift; rebuild authorizes Bootwright-owned destructive rebuilds of drifted owned objects (a rebuild that destroys data additionally needs --authorize "+authorizeDataLoss+")")
	registerFlagCompletion(cmd, "mode", workflow.ApplyModeNames())
}

func parseApplyMode(value string) (workflow.ApplyMode, error) {
	mode := workflow.ApplyMode(strings.TrimSpace(value))
	if !mode.Valid() {
		return "", fmt.Errorf("--mode must be one of %s", strings.Join(workflow.ApplyModeNames(), ", "))
	}
	return mode, nil
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

func resolveSSHUser() (string, error) {
	user := strings.TrimSpace(sshUserOverride)
	if user == "" {
		if sshUserForProvisioned {
			return "", fmt.Errorf("--ssh-user-for-provisioned widens --ssh-user to the machines Bootwright installed, but no --ssh-user was given, so there is no account to widen. Pass --ssh-user <name>, or drop the flag")
		}
		return "", nil
	}
	if !v1alpha1.ValidPOSIXUserName(user) {
		return "", fmt.Errorf("--ssh-user %q is not a valid POSIX user name (lowercase letter or underscore, then lowercase letters, digits, underscore or dash, at most 32 chars)", sshUserOverride)
	}
	return user, nil
}
