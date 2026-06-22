package converge

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

// runOptionsForContext seeds a workflow.RunOptions with the fields every
// converge run derives from the same sources: the workspace.Context directories,
// the State, the clusters dir, and the ansible executable. Apply, destroy,
// dry-run, and preflight overlay only their run-specific fields (playbook,
// limit, mode, lease, ...) onto the returned value, so the shared context
// mapping lives in exactly one place instead of being hand-copied per site.
func runOptionsForContext(ctx workspace.Context, clustersDir, executable string, state v1alpha1.State) workflow.RunOptions {
	return workflow.RunOptions{
		State:              state,
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		ContextName:        ctx.Name,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		OwnershipDir:       ctx.OwnershipDir,
		Executable:         executable,
	}
}
