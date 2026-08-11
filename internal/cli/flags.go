package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

var globalValueFlags = []string{"--context", "--ssh-user", "--ssh-id-file"}

const flagSSHAskSudoPassword = "--ssh-ask-sudo-password"

var globalBoolFlags = []string{flagSSHAskSudoPassword, "--ssh-user-for-provisioned"}

func stripLeadingGlobalFlags(args []string) []string {
	return args[leadingGlobalFlagCount(args):]
}

func leadingGlobalFlagCount(args []string) int {
	i := 0
	for i < len(args) {
		if leadingGlobalBoolFlag(args[i]) {
			i++
			continue
		}
		flag, ok := leadingGlobalValueFlag(args[i])
		if !ok {
			return i
		}
		if args[i] == flag {
			if i+1 >= len(args) {
				return len(args)
			}
			i += 2
			continue
		}
		i++
	}
	return i
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

func argsNeedCallerSudoPassword(args []string) bool {
	return argsMayUseBecome(stripLeadingGlobalFlags(args)) || argsAskSSHSudoPassword(args)
}

func argsAskSSHSudoPassword(args []string) bool {
	for _, arg := range argsBeforeCommandPayload(args) {
		name, value, hasValue := strings.Cut(arg, "=")
		if name != flagSSHAskSudoPassword {
			continue
		}
		if !hasValue {
			return true
		}
		enabled, err := strconv.ParseBool(value)
		return err == nil && enabled
	}
	return false
}

const (
	outputText = "text"
	outputJSON = "json"

	flagOutputUsage       = "output format (text|json)"
	flagOutputDryRunUsage = "output format (text|json); json requires --dry-run"

	flagDryRunUsage                    = "render artifacts and print the plan; change nothing remote"
	flagClusterInstallParallelismUsage = "maximum ContainerCluster install chains in flight; must be positive; default: every chain allowed by the selected dependency graph"
	flagAskBecomePassUsage             = "prompt for the Ansible become password (default: false as root, true otherwise)"
	flagTrustOnFirstUseUsage           = "prompt to record an unknown SSH host key after showing its fingerprint (interactive runs only; never under --dry-run, --yes, or --output json)"
	flagContextUsage                   = "context to operate in (default: current context)"
	flagSSHIDFileUsage                 = "SSH private key to offer first when reaching machines (for example ~/.ssh/id_ed25519); the declared spec.access.ssh credentials are still offered when it is not accepted"
	flagSSHUserUsage                   = "account to log in as on machines that declare spec.access.ssh.auth.operatorIdentity — the machines you already administer; a login Bootwright created or one you named a Secret for is unaffected unless --ssh-user-for-provisioned widens it, and apply/plan/destroy refuse when no selected machine uses that arm; on rsh and exec it reaches any account, and one Bootwright already holds a credential for is opened with that credential rather than with the credential of the account it replaced"
	flagSSHAskSudoPasswordUsage        = "prompt once, before the run starts, for the sudo password of the account --ssh-user names, and answer sudo with it on the machines that account reaches; when bootwright already prompted that same account for its local sudo password to reach its own state directory, that answer is reused and you are not asked twice; the password is held in memory for the run only and is never written to the context secret store, the rendered inventory, or the run log"
	flagSSHUserForProvisionedUsage     = "widen --ssh-user to every machine in the run, including the ones Bootwright installed and reaches as \"" + v1alpha1.BootwrightSSHUser + "\"; requires --ssh-user, and the named account must exist with sudo on those machines too, because the managed-OS ownership probe authenticates as whatever account is in force"
	flagVerboseUsage                   = "print full Ansible task output, including values normally hidden as \"censored due to no_log\" (secrets, BMC/registry/RHSM/proxy credentials, tokens, generated Ceph keys); WARNING: these are written to the terminal AND the run log"
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
	cmd.Flags().StringVar(p, "mode", string(workflow.ApplyModeReconcile), "intent of this "+action+" ("+strings.Join(workflow.ApplyModeNames(), "|")+"): create asserts a greenfield run and fails if any selected object already exists; reconcile creates what is missing, skips what matches, converges drift that is reconcilable in place, and fails closed on structural (destructive-identity) drift or foreign ownership; rebuild authorizes Bootwright-owned destructive rebuilds of drifted owned objects (a rebuild that destroys data additionally needs --authorize "+authorizeDataLoss+")")
	registerFlagCompletion(cmd, "mode", workflow.ApplyModeNames())
}

func addClusterInstallParallelismFlag(cmd *cobra.Command, p *int) {
	cmd.Flags().IntVar(p, "cluster-install-parallelism", 0, flagClusterInstallParallelismUsage)
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
