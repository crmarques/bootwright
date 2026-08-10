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
	taskOpts.OutputLogPath = TaskLogPath(runsDir, runID, task.Entry)
	taskOpts.AcquireRunLease = false
	taskOpts.SkipNoHostsBeforeRender = true
	if runnerFactory == nil {
		runnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	runner := runnerFactory(stdout, stderr)
	result, err := Run(ctx, taskOpts, runner, nil)
	var finalize func() error
	if err == nil && task.Entry.Kind == DestroyTaskKindStorageCluster {
		finalize, err = validateStorageDestroyTask(taskOpts, task)
	}
	if err != nil {
		failure := conciseApplyTaskFailure(err)
		logPath := TaskLogPath(runsDir, runID, task.Entry)
		return applyTaskResult{
			id:      task.Entry.ID,
			skipped: result.Skipped,
			failure: failure,
			err:     fmt.Errorf("%s failed: %s (log: %s)", task.Entry.Label, failure, logPath),
		}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, finalize: finalize}
}

func validateStorageDestroyTask(opts RunOptions, task ApplyTask) (func() error, error) {
	expected := StorageDestroyExpectedNodes(task.State, task.Entry.ResourceKeys)
	expectedSeedHosts := StorageDestroyExpectedSeedHosts(task.State, task.Entry.ResourceKeys)
	if len(expected) == 0 {
		return nil, nil
	}
	path := filepath.Join(opts.ArtifactsRoot, StorageDestroyResultFileName)
	report, found, err := ReadStorageDestroyResult(path)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("storage teardown produced no completion attestation at %s", path)
	}
	allowSkipped := extraVarValue(opts.ExtraVarPairs, storageDestroySkipUnreachableExtraVar) == "true"
	results, err := ValidateStorageDestroyResults([]StorageDestroyResult{report}, expected, allowSkipped)
	if err != nil {
		return nil, err
	}
	return func() error {
		return ReconcileStorageDestroyOwnership(opts.OwnershipDir, opts.ContextName, results, expectedSeedHosts)
	}, nil
}
