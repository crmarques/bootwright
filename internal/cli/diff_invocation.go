package cli

import "strings"

func (i resolvedInvocation) diffArgs() []string {
	args := []string{"bootwright", "diff"}
	if value := strings.TrimSpace(i.flags.selection.stage); value != "" {
		args = append(args, "--stage", value)
	}
	if value := strings.TrimSpace(i.flags.selection.through); value != "" {
		args = append(args, "--through", value)
	}
	if value := strings.TrimSpace(i.flags.selection.clusters); value != "" {
		args = append(args, "--clusters", value)
	}
	if i.flags.recorded {
		args = append(args, "--recorded")
	}
	if i.flags.adopt {
		args = append(args, "--adopt")
	}
	if i.flags.output != "" && i.flags.output != outputText {
		args = append(args, "--output", i.flags.output)
	}
	if i.flags.verbose {
		args = append(args, "--verbose")
	}
	if i.contextName != "" {
		args = append(args, "--context", i.contextName)
	}
	if i.sshIdentityFile != "" {
		args = append(args, "--ssh-id-file", i.sshIdentityFile)
	}
	if i.sshUser != "" {
		args = append(args, "--ssh-user", i.sshUser)
	}
	if i.sshAskSudoPassword {
		args = append(args, "--ssh-ask-sudo-password")
	}
	if i.sshUserForProvisioned {
		args = append(args, "--ssh-user-for-provisioned")
	}
	return args
}
