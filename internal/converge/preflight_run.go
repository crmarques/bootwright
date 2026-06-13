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
// surface behind the workflow.Reporter interface. Ansible output is written to
// the returned log path only; the verbose stream reaches the terminal solely
// when streamAnsible is set (--stream-ansible). The structured, human-facing
// readiness report comes from the Go-level host checks the CLI runs first.
func RunScopePreflight(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, scope Scope, state v1alpha1.State, limit string, dryRun bool, streamAnsible bool, reporter workflow.Reporter) (string, error) {
	logPath := workflow.PreflightLogPath(ctx.RunsDir, scope.Name)
	runner := preflightRunner(stdout, stderr, streamAnsible)
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
		OutputLogPath:      logPath,
		DryRun:             dryRun,
		Label:              scope.Name + " preflight",
	}, runner, reporter)
	return logPath, err
}

// preflightRunner routes ansible output to the per-task log only by default,
// keeping the terminal clean for the structured checks and the progress view.
// With streamAnsible it tees raw output to the terminal as well.
func preflightRunner(stdout, stderr io.Writer, streamAnsible bool) ansible.CommandRunner {
	if streamAnsible {
		return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
	}
	return ansible.CommandRunner{}
}
