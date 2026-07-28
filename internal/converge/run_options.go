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
)

func SetPreferredIdentityFile(path string) {
	preferredIdentityFile = path
}

func SetSSHUser(user string) {
	sshUser = user
}

func checkSSHUserScope(state v1alpha1.State) error {
	if sshUser == "" {
		return nil
	}
	for _, machine := range state.Machines {
		if v1alpha1.MachineUsesOperatorIdentity(machine) {
			return nil
		}
	}
	return fmt.Errorf("--ssh-user %q applies only to machines that declare spec.access.ssh.auth.operatorIdentity, and no machine in this run does. Bootwright owns the %q login on the machines it installs and connects to every other machine with the credential its Secret names, so the flag would change nothing. Drop it, or narrow the run to a machine you already administer", sshUser, v1alpha1.BootwrightSSHUser)
}

func runOptionsForContext(ctx workspace.Context, clustersDir, executable string, state v1alpha1.State) workflow.RunOptions {
	return workflow.RunOptions{
		State:                 state,
		PreferredIdentityFile: preferredIdentityFile,
		SSHUser:               sshUser,
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
