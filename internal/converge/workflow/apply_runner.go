package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/secrets"
)

func runOneApplyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	result := runOneApplyTaskInner(ctx, stdout, stderr, runsDir, runID, opts, task, runnerFactory)
	if result.err == nil {
		return result
	}
	failure := conciseApplyTaskFailure(result.err.Error())
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	taskErr := fmt.Errorf("%s failed: %s (log: %s)", task.Entry.Label, failure, logPath)
	if logErr := ensureApplyTaskFailureLog(logPath, failure); logErr != nil {
		taskErr = fmt.Errorf("%w; additionally failed to write task log %s: %v", taskErr, logPath, logErr)
	}
	result.failure = failure
	result.err = taskErr
	return result
}

func ensureApplyTaskFailureLog(path, failure string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.Mode().IsRegular() && info.Size() > 0:
		return nil
	case err == nil && !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("stat %s: %w", path, err)
	}
	return safefs.WriteFileEnsuringDir(path, []byte("failure: "+failure+"\n"), 0o600)
}

func runOneApplyTaskInner(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.Entry.Kind == ApplyTaskKindClusterAddon {
		return runOneExtensionTask(ctx, stdout, stderr, runsDir, runID, opts, task, runnerFactory)
	}
	if task.Entry.Kind == ApplyTaskKindNodeConfigApply {
		return runOneNodeConfigTask(ctx, stdout, stderr, runsDir, runID, opts, task)
	}
	if skipped, reason, err := provisioningPlaybookConvergeSkip(runsDir, opts.ContextName, runID, task); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	} else if skipped {
		return applyTaskResult{id: task.Entry.ID, skipped: true, skippedReason: reason}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	taskOpts := opts
	taskOpts.State = task.State
	taskOpts.RenderDir = renderDir
	taskOpts.Playbook = task.Playbook
	taskOpts.Limit = task.Limit
	taskOpts.RolesPath = task.RolesPath
	taskOpts.CollectionsPath = task.CollectionsPath
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
	_, isInstallStartKind := clusterInstallTaskStartPhase(task.Entry.Kind)
	restorable := isInstallStartKind && task.Entry.Cluster != "" && stateHasContainerCluster(task.State, task.Entry.Cluster)
	var priorInstall ClusterInstallRecord
	var priorInstallFound bool
	if restorable {
		var loadErr error
		priorInstall, priorInstallFound, loadErr = LoadClusterInstallRecord(opts.ClustersDir, task.Entry.Cluster)
		if loadErr != nil {
			return applyTaskResult{id: task.Entry.ID, err: loadErr}
		}
	}
	if err := MarkClusterInstallTaskStarted(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	result, err := Run(ctx, taskOpts, runner, nil)
	now = time.Now()
	if err != nil {
		if recordErr := MarkClusterInstallTaskFailed(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); recordErr != nil {
			err = fmt.Errorf("%w; additionally failed to record cluster install state: %v", err, recordErr)
		}
		if remErr := RemoveApplyTaskConvergeSafety(runsDir, task); remErr != nil {
			err = fmt.Errorf("%w; additionally failed to reset the task's converge record (it may still claim the previous inputs are applied): %v", err, remErr)
		}
		if task.Entry.Kind == ApplyTaskKindStorageCluster {
			if remErr := RemoveStorageSubObjectsConvergeSafety(runsDir, task.State, strings.TrimPrefix(task.Entry.ID, "storage.")); remErr != nil {
				err = fmt.Errorf("%w; additionally failed to reset storage sub-object converge records: %v", err, remErr)
			}
		}
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
	}
	if result.Skipped && restorable {
		if restoreErr := restoreClusterInstallRecordOnSkip(opts.ClustersDir, task.Entry.Cluster, priorInstall, priorInstallFound); restoreErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: restoreErr}
		}
	}
	if result.Skipped {
		return applyTaskResult{id: task.Entry.ID, skipped: true, err: nil}
	}
	if task.Entry.Kind == ApplyTaskKindStorageCluster || task.Entry.Kind == ApplyTaskKindInstallWait || task.Entry.Kind == ApplyTaskKindBootstrapWait {
		if err := encryptCapturedClusterSecrets(opts, task); err != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
		}
	}
	if task.Entry.Kind == ApplyTaskKindStorageCluster {
		clusterName := strings.TrimPrefix(task.Entry.ID, "storage.")
		if recordErr := MarkStorageSubObjectsConvergeSafety(runsDir, opts.ContextName, runID, task.State, clusterName, ConvergeSafetyStatusReconciled, now); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	if recordErr := MarkClusterInstallTaskSucceeded(opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, task, now); recordErr != nil {
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
	}
	if recordErr := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, ConvergeSafetyStatusReconciled, now); recordErr != nil {
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
	}
	if SubstrateReleaseClearKind(task.Entry.Kind) {
		if recordErr := ConsumeSubstrateRelease(runsDir, task.Entry.Cluster, opts.SelectedMachines, ClusterSubstrateMachineNames(task.State, task.Entry.Cluster)); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
}

func encryptCapturedClusterSecrets(opts RunOptions, task ApplyTask) error {
	clusterName := task.Entry.Cluster
	if clusterName == "" && task.Entry.Kind == ApplyTaskKindStorageCluster {
		clusterName = strings.TrimPrefix(task.Entry.ID, "storage.")
	}
	if clusterName == "" {
		return nil
	}
	store := secret.NewContextStore(effectiveContextName(opts.ContextName), ClusterSecretsDir(opts.ClustersDir, clusterName))
	names := []string{"kubeconfig", "kubeadmin-password"}
	if task.Entry.Kind == ApplyTaskKindStorageCluster {
		names = []string{"dashboard-password"}
	}
	for _, name := range names {
		if _, err := store.MigratePlaintextMaterial(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}); err != nil {
			return fmt.Errorf("encrypt captured secret %s for cluster %s: %w", name, clusterName, err)
		}
	}
	return nil
}

func restoreClusterInstallRecordOnSkip(clustersDir, cluster string, prior ClusterInstallRecord, priorFound bool) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	if priorFound {
		return SaveClusterInstallRecord(clustersDir, prior)
	}
	if err := os.Remove(ClusterInstallRecordPath(clustersDir, cluster)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove phantom cluster install record %s: %w", ClusterInstallRecordPath(clustersDir, cluster), err)
	}
	return nil
}

func provisioningPlaybookConvergeSkip(runsDir, contextName, runID string, task ApplyTask) (bool, string, error) {
	if !task.SkipWhenConverged || strings.TrimSpace(runsDir) == "" {
		return false, "", nil
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil || !found {
		return false, "", nil
	}
	if record.Status != ConvergeSafetyStatusReconciled && record.Status != ConvergeSafetyStatusSkipped {
		return false, "", nil
	}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil || record.DesiredHash != desiredHash {
		return false, "", nil
	}
	if err := MarkApplyTaskConvergeSafety(runsDir, contextName, runID, task, ConvergeSafetyStatusSkipped, time.Now()); err != nil {
		return false, "", err
	}
	return true, "inputs unchanged since last run (run: onChange)", nil
}
