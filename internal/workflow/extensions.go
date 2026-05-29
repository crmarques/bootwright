package workflow

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/crmarques/bootwright/internal/clusterextensions"
)

func runOneExtensionTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	if task.Extension == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("extension task %s has no extension plan", task.Entry.ID)}
	}
	kubeconfig := clusterKubeconfigPath(opts.RuntimeDir, opts.State, task.Entry.Cluster)
	runner := clusterextensions.CommandRunner{
		LogPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	cfg := clusterextensions.RunConfig{
		RuntimeDir:   opts.RuntimeDir,
		Kubeconfig:   kubeconfig,
		RunID:        runID,
		StartedAt:    time.Now(),
		PollInterval: 0,
	}
	var result clusterextensions.TaskResult
	var err error
	switch task.Entry.Kind {
	case ApplyTaskKindClusterExtensionApply:
		result, err = clusterextensions.Apply(ctx, runner, cfg, *task.Extension)
	case ApplyTaskKindClusterExtensionWait:
		result, err = clusterextensions.Wait(ctx, runner, cfg, *task.Extension)
	default:
		err = fmt.Errorf("unsupported extension task kind %s", task.Entry.Kind)
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, skippedReason: result.Reason, err: err}
}
