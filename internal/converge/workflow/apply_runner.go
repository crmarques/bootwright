package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	storageapply "github.com/crmarques/bootwright/internal/storage"
)

func runOneApplyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	result := runOneApplyTaskInner(ctx, stdout, stderr, runsDir, runID, opts, task, runnerFactory)
	if result.err == nil {
		return result
	}
	failure := conciseApplyTaskFailure(result.err.Error())
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	result.failure = failure
	result.err = fmt.Errorf("%s failed: %s (log: %s)", task.Entry.Label, failure, logPath)
	return result
}

func runOneApplyTaskInner(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.Entry.Kind == ApplyTaskKindClusterAddon {
		return runOneExtensionTask(ctx, stdout, stderr, runsDir, runID, opts, task)
	}
	if task.Entry.Kind == ApplyTaskKindStorageAttachmentApply {
		return runOneStorageAttachmentTask(ctx, stdout, stderr, runsDir, runID, opts, task, runnerFactory)
	}
	if task.Entry.Kind == ApplyTaskKindNodeConfigApply {
		return runOneNodeConfigTask(ctx, stdout, stderr, runsDir, runID, opts, task)
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	taskOpts := opts
	taskOpts.State = task.State
	taskOpts.RenderDir = renderDir
	taskOpts.Playbook = task.Playbook
	taskOpts.Limit = task.Limit
	taskOpts.ExtraVarPairs = append(append([]string(nil), opts.ExtraVarPairs...), task.ExtraVarPairs...)
	taskOpts.ArtifactsBaseName = ""
	taskOpts.ResolveInstaller = false
	taskOpts.Label = task.Entry.Label
	taskOpts.Forks = task.Forks
	taskOpts.ArtifactsRoot = filepath.Join(taskRoot, "artifacts")
	taskOpts.OutputLogPath = TaskLogPath(runsDir, runID, task.Entry.ID)
	if runnerFactory == nil {
		runnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	runner := runnerFactory(stdout, stderr)
	now := time.Now()
	if err := MarkClusterInstallTaskStarted(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	result, err := Run(ctx, taskOpts, runner, nil)
	now = time.Now()
	if err != nil {
		if recordErr := MarkClusterInstallTaskFailed(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); recordErr != nil {
			err = fmt.Errorf("%w; additionally failed to record cluster install state: %v", err, recordErr)
		}
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
	}
	if task.Entry.Kind == ApplyTaskKindStorageCluster && !result.Skipped {
		clusterName := strings.TrimPrefix(task.Entry.ID, "storage.")
		if err := storageapply.PersistCephApplyResult(storageapply.CephApplyResultOptions{
			State:              task.State,
			ClustersDir:        opts.ClustersDir,
			StorageClusterName: clusterName,
			ResultPath:         filepath.Join(taskOpts.ArtifactsRoot, "storage-result.json"),
		}); err != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
		}
		// Record each pool/filesystem/gateway/export so the next apply's preflight and
		// state-check report sub-object drift independently of the cluster. Runs for
		// every storage cluster, not only those with dataFoundation bindings.
		if recordErr := MarkStorageSubObjectsConvergeSafety(runsDir, opts.ContextName, runID, task.State, clusterName, ConvergeSafetyStatusReconciled, now); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	if !result.Skipped {
		if recordErr := MarkClusterInstallTaskSucceeded(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	safetyStatus := ConvergeSafetyStatusReconciled
	if result.Skipped {
		safetyStatus = ConvergeSafetyStatusSkipped
	}
	if recordErr := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, safetyStatus, now); recordErr != nil {
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
}
