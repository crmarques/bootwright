package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func runOneDestroyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	taskOpts := opts
	taskOpts.State = task.State
	taskOpts.RenderDir = filepath.Join(taskRoot, "rendered")
	taskOpts.Playbook = task.Playbook
	taskOpts.Limit = task.Limit
	taskOpts.ExtraVarPairs = append(append([]string(nil), opts.ExtraVarPairs...), task.ExtraVarPairs...)
	taskOpts.ArtifactsBaseName = ""
	taskOpts.ResolveInstaller = false
	taskOpts.Label = task.Entry.Label
	taskOpts.Forks = task.Forks
	taskOpts.ArtifactsRoot = filepath.Join(taskRoot, "artifacts")
	taskOpts.OutputLogPath = TaskLogPath(runsDir, runID, task.Entry.ID)
	taskOpts.AcquireRunLease = false
	if runnerFactory == nil {
		runnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	runner := runnerFactory(stdout, stderr)
	result, err := Run(ctx, taskOpts, runner, nil)
	if err != nil {
		failure := conciseApplyTaskFailure(err.Error())
		logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
		return applyTaskResult{
			id:      task.Entry.ID,
			skipped: result.Skipped,
			failure: failure,
			err:     fmt.Errorf("%s failed: %s (log: %s)", task.Entry.Label, failure, logPath),
		}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped}
}
