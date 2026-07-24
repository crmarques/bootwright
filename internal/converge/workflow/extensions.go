package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	"github.com/crmarques/bootwright/internal/host/safefs"
)

func runOneExtensionTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.Extension == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("add-on task %s has no add-on plan", task.Entry.ID)}
	}
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	var result extensionoc.TaskResult
	err := withMaterializedClusterKubeconfig(opts.ContextName, opts.ClustersDir, task.Entry.Cluster, func(kubeconfig string) error {
		runner := extensionoc.CommandRunner{
			LogPath: logPath,
			Stdout:  stdout,
			Stderr:  stderr,
		}
		readRunner := extensionoc.CommandRunner{}
		cfg := extensionoc.RunConfig{
			ClustersDir:  opts.ClustersDir,
			Kubeconfig:   kubeconfig,
			RunID:        runID,
			StartedAt:    time.Now(),
			PollInterval: 0,
			ReadRunner:   readRunner,
			Progress:     io.MultiWriter(stdout, addonProgressLogWriter(logPath)),
			Hooks:        newAddonHookExecutor(stdout, stderr, runsDir, runID, kubeconfig, opts, task, runnerFactory),
			Effects:      newAddonEffectExecutor(stdout, stderr, runsDir, runID, opts, task),
		}
		switch task.Entry.Kind {
		case ApplyTaskKindClusterAddon:
			plan := *task.Extension
			hash, err := plan.ComputeDesiredHash()
			if err != nil {
				return err
			}
			plan.DesiredHash = hash
			applied, err := extensionoc.Apply(ctx, runner, cfg, plan)
			if err != nil {
				return err
			}
			waited, err := extensionoc.Wait(ctx, runner, cfg, plan)
			if err != nil {
				return err
			}
			if applied.Skipped && waited.Skipped {
				result = extensionoc.TaskResult{Skipped: true, Reason: applied.Reason}
			}
			return nil
		default:
			return fmt.Errorf("unsupported add-on task kind %s", task.Entry.Kind)
		}
	})
	if err == nil {
		status := ConvergeSafetyStatusReconciled
		if result.Skipped {
			status = ConvergeSafetyStatusSkipped
		}
		err = MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, status, time.Now())
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, skippedReason: result.Reason, err: err}
}

type addonProgressLogWriter string

func (w addonProgressLogWriter) Write(data []byte) (int, error) {
	path := string(w)
	if err := safefs.EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return 0, err
	}
	n, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return n, writeErr
	}
	return n, closeErr
}
