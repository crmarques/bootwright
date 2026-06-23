package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// runOneNodeConfigTask applies a cluster's day-2 node configuration: the infra
// role labels/taints and the infra MachineConfigPool (plus any authored
// labels/taints). It generates the manifests in-process from the desired host
// specs and applies them with server-side `oc apply` against the installed
// cluster's kubeconfig — the same shape as runOneStorageAttachmentTask. The
// MachineConfigPool roll (which reboots nodes) proceeds asynchronously in the
// cluster; this task does not block on it.
func runOneNodeConfigTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	ocp, ok := stateview.ContainerCluster(task.State, task.Entry.Cluster)
	if !ok {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("node config task %s: cluster %q not in task state", task.Entry.ID, task.Entry.Cluster)}
	}
	manifests, err := nodeConfigManifests(ocp)
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	if len(manifests) == 0 {
		// Nothing to do (e.g. infra role removed); record reconciled.
		if err := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, ConvergeSafetyStatusReconciled, time.Now()); err != nil {
			return applyTaskResult{id: task.Entry.ID, err: err}
		}
		return applyTaskResult{id: task.Entry.ID, skipped: true, skippedReason: "no node config to apply"}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	if err := os.MkdirAll(renderDir, 0o755); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	manifestPath := filepath.Join(renderDir, "node-config.yaml")
	if err := os.WriteFile(manifestPath, manifests, 0o600); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	runner := extensionoc.CommandRunner{
		LogPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	if _, err := runner.Run(ctx, kubeconfig, []string{"apply", "-f", manifestPath, "--server-side", "--field-manager", "bootwright"}, nil); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	if err := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, ConvergeSafetyStatusReconciled, time.Now()); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	return applyTaskResult{id: task.Entry.ID}
}
