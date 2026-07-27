package converge

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/workspace"
)

func RunScopePreflight(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, scope Scope, state v1alpha1.State, limit string, dryRun bool, verbose bool, streamAnsible bool, reporter workflow.Reporter) (string, error) {
	logPath := workflow.PreflightLogPath(ctx.RunsDir, scope.Name)
	runner := preflightRunner(stdout, stderr, streamAnsible)
	opts := runOptionsForContext(ctx, clustersDir, executable, state)
	opts.BundleDir = bundleDir
	opts.Playbook = PreflightPlaybook
	opts.Limit = limit
	opts.ArtifactsBaseName = "preflight-" + scope.Name
	opts.OutputLogPath = logPath
	opts.DryRun = dryRun
	opts.ExtraVarPairs = VerboseNoLogExtraVarPairs(verbose)
	opts.Forks = PreflightForks(state, limit)
	opts.Label = scope.Name + " preflight"
	_, err := workflow.Run(cmdCtx, opts, runner, reporter)
	return logPath, err
}

const PreflightMaxForks = 20

func PreflightForks(state v1alpha1.State, limit string) int {
	forks := workflow.AnsibleForksForLimit(state, limit)
	if forks > PreflightMaxForks {
		return PreflightMaxForks
	}
	if forks < 1 {
		return 1
	}
	return forks
}

func preflightRunner(stdout, stderr io.Writer, streamAnsible bool) ansible.CommandRunner {
	if streamAnsible {
		return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
	}
	return ansible.CommandRunner{}
}
