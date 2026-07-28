package converge

import (
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
