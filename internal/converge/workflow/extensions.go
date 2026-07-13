package workflow

import (
	"context"
	"fmt"
	"io"
	"time"

	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
)

func runOneExtensionTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.Extension == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("add-on task %s has no add-on plan", task.Entry.ID)}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	runner := extensionoc.CommandRunner{
		LogPath: logPath,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	readRunner := extensionoc.CommandRunner{LogPath: logPath}
	cfg := extensionoc.RunConfig{
		ClustersDir:  opts.ClustersDir,
		Kubeconfig:   kubeconfig,
		RunID:        runID,
		StartedAt:    time.Now(),
		PollInterval: 0,
		ReadRunner:   readRunner,
		Hooks:        newAddonHookExecutor(stdout, stderr, runsDir, runID, opts, task, runnerFactory),
		Effects:      newAddonEffectExecutor(stdout, stderr, runsDir, runID, opts, task),
	}
	var result extensionoc.TaskResult
	var err error
	switch task.Entry.Kind {
	case ApplyTaskKindClusterAddon:
		plan := *task.Extension
		var hash string
		if hash, err = plan.ComputeDesiredHash(); err == nil {
			plan.DesiredHash = hash
			var applied, waited extensionoc.TaskResult
			applied, err = extensionoc.Apply(ctx, runner, cfg, plan)
			if err == nil {
				waited, err = extensionoc.Wait(ctx, runner, cfg, plan)
			}
			if err == nil && applied.Skipped && waited.Skipped {
				result = extensionoc.TaskResult{Skipped: true, Reason: applied.Reason}
			}
		}
	default:
		err = fmt.Errorf("unsupported add-on task kind %s", task.Entry.Kind)
	}
	if err == nil {
		status := ConvergeSafetyStatusReconciled
		if result.Skipped {
			status = ConvergeSafetyStatusSkipped
		}
		err = MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, status, time.Now())
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, skippedReason: result.Reason, err: err}
}
