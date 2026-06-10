package converge

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

// RunScopePreflight runs (or dry-runs) the read-only Ansible preflight for a
// scope against the already-scoped state. The reporter is the CLI's progress
// surface behind the workflow.Reporter interface.
func RunScopePreflight(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, scope Scope, state v1alpha1.State, limit string, dryRun bool, reporter workflow.Reporter) error {
	runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
	_, err := workflow.Run(cmdCtx, workflow.RunOptions{
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
		BundleDir:          bundleDir,
		Playbook:           PreflightPlaybook,
		Limit:              limit,
		ArtifactsBaseName:  "preflight-" + scope.Name,
		DryRun:             dryRun,
		Label:              scope.Name + " preflight",
	}, runner, reporter)
	return err
}
