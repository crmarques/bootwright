package workflow

import (
	"context"
	"fmt"
	"io"
	"time"

	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
)

func runOneExtensionTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	if task.Extension == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("addon task %s has no addon plan", task.Entry.ID)}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	runner := extensionoc.CommandRunner{
		LogPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	cfg := extensionoc.RunConfig{
		ClustersDir:  opts.ClustersDir,
		Kubeconfig:   kubeconfig,
		RunID:        runID,
		StartedAt:    time.Now(),
		PollInterval: 0,
	}
	var result extensionoc.TaskResult
	var err error
	switch task.Entry.Kind {
	case ApplyTaskKindClusterAddon:
		// Install the addon, then wait for it to report ready. The task is only
		// "skipped" (nothing converged) when both phases are no-ops.
		var applied, waited extensionoc.TaskResult
		applied, err = extensionoc.Apply(ctx, runner, cfg, *task.Extension)
		if err == nil {
			waited, err = extensionoc.Wait(ctx, runner, cfg, *task.Extension)
		}
		if err == nil && applied.Skipped && waited.Skipped {
			result = extensionoc.TaskResult{Skipped: true, Reason: applied.Reason}
		}
	default:
		err = fmt.Errorf("unsupported addon task kind %s", task.Entry.Kind)
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
