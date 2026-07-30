package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

func offerAccountIdentity(entry map[string]any, accountPrivateKeyPath string) {
	if accountPrivateKeyPath == "" {
		return
	}
	identity := shellquote.QuoteWords([]string{"-o", "IdentityFile=" + accountPrivateKeyPath})
	existing, _ := entry["ansible_ssh_common_args"].(string)
	entry["ansible_ssh_common_args"] = strings.TrimSpace(identity + " " + existing)
	if declared, ok := entry["bootwright_declared_ssh_common_args"].(string); ok {
		entry["bootwright_declared_ssh_common_args"] = strings.TrimSpace(identity + " " + declared)
	}
}

func sshUserApplies(machine v1alpha1.Machine, paths PathOptions) bool {
	if paths.SSHUser == "" || machine.Spec.Access.SSH == nil {
		return false
	}
	return paths.SSHUserForProvisioned || v1alpha1.MachineUsesOperatorIdentity(machine)
}

func sshSudoPasswordApplies(machine v1alpha1.Machine, paths PathOptions) bool {
	if !paths.AskSSHSudoPassword {
		return false
	}
	return v1alpha1.MachineUsesOperatorIdentity(machine) || sshUserApplies(machine, paths)
}

func connectionUser(machine v1alpha1.Machine, paths PathOptions) string {
	if sshUserApplies(machine, paths) {
		return paths.SSHUser
	}
	if machine.Spec.Access.SSH == nil {
		return ""
	}
	return machine.Spec.Access.SSH.User
}

func passwordLookup(path string) string {
	return "{{ lookup('ansible.builtin.file', '" + path + "') | trim }}"
}

func envLookup(name string) string {
	return "{{ lookup('ansible.builtin.env', '" + name + "') }}"
}

func machineKnownHostsPath(h v1alpha1.Machine, paths PathOptions) string {
	return sshtrust.MachineKnownHostsPath(h, paths.SecretIndex, paths.SecretsDir, sshtrust.KnownHostsPathForSecrets(paths.trustSecretsDir()))
}

func sshCommonArgs(knownHostsPath string, passwordAuth bool, preferredIdentityFile string) string {
	return shellquote.QuoteWords(SSHCommonArgWords(knownHostsPath, passwordAuth, preferredIdentityFile))
}

func SSHCommonArgWords(knownHostsPath string, passwordAuth bool, preferredIdentityFile string) []string {
	words := []string{}
	if preferredIdentityFile != "" {
		words = append(words, "-o", "IdentityFile="+preferredIdentityFile)
	}
	if !passwordAuth {
		words = append(words, "-o", "BatchMode=yes")
	}
	return append(words,
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile="+knownHostsPath,
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	)
}
