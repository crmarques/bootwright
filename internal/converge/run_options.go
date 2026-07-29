package converge

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

var (
	preferredIdentityFile string
	sshUser               string
	sshSudoPassword       string
	sshUserForProvisioned bool
)

func SetPreferredIdentityFile(path string) {
	preferredIdentityFile = path
}

func SetSSHUser(user string) {
	sshUser = user
}

func SetSSHSudoPassword(password string) {
	sshSudoPassword = password
}

func SetSSHUserForProvisioned(widen bool) {
	sshUserForProvisioned = widen
}

func checkSSHUserScope(state v1alpha1.State) error {
	if sshUser == "" && sshSudoPassword == "" {
		return nil
	}
	if sshUserForProvisioned && sshUser != "" {
		return nil
	}
	for _, machine := range state.Machines {
		if v1alpha1.MachineUsesOperatorIdentity(machine) {
			return nil
		}
	}
	if sshUser != "" {
		return fmt.Errorf("--ssh-user %q applies only to machines that declare spec.access.ssh.auth.operatorIdentity, and no machine in this run does. Bootwright owns the %q login on the machines it installs and connects to every other machine with the credential its Secret names, so the flag would change nothing. Drop it, or narrow the run to a machine you already administer", sshUser, v1alpha1.BootwrightSSHUser)
	}
	return fmt.Errorf("--ssh-ask-sudo-password applies only to machines that declare spec.access.ssh.auth.operatorIdentity, and no machine in this run does. It answers the sudo prompt of the account you already administer; the %q login Bootwright creates holds a passwordless grant it proves before it relies on it, so the prompted password would never be used. Drop it, or narrow the run to a machine you already administer", v1alpha1.BootwrightSSHUser)
}

func runOptionsForContext(ctx workspace.Context, clustersDir, executable string, state v1alpha1.State) workflow.RunOptions {
	return workflow.RunOptions{
		State:                 state,
		PreferredIdentityFile: preferredIdentityFile,
		SSHUser:               sshUser,
		SSHSudoPassword:       sshSudoPassword,
		SSHUserForProvisioned: sshUserForProvisioned,
		RenderedDir:           ctx.RenderedDir,
		ClustersDir:           clustersDir,
		RunsDir:               ctx.RunsDir,
		ContextName:           ctx.Name,
		SecretsDir:            ctx.SecretsDir,
		ManagedServicesDir:    ctx.ManagedServicesDir,
		ProviderStateDir:      ctx.ProviderStateDir,
		OwnershipDir:          ctx.OwnershipDir,
		Executable:            executable,
	}
}
