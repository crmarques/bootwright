package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	nodeRegistrationAttempts = 30
	nodeRegistrationInterval = 10 * time.Second
)

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
		if err := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, ConvergeSafetyStatusReconciled, time.Now()); err != nil {
			return applyTaskResult{id: task.Entry.ID, err: err}
		}
		return applyTaskResult{id: task.Entry.ID, skipped: true, skippedReason: "no node config to apply"}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	if err := os.MkdirAll(renderDir, 0o700); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	manifestPath := filepath.Join(renderDir, "node-config.yaml")
	if err := os.WriteFile(manifestPath, manifests, 0o600); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	checker := extensionoc.CommandRunner{LogPath: logPath}
	if err := waitNodesRegistered(ctx, checker, kubeconfig, task.Entry.Cluster, nodeConfigNodeNames(ocp), nodeRegistrationAttempts, nodeRegistrationInterval); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	runner := extensionoc.CommandRunner{
		LogPath: logPath,
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

func nodeConfigNodeNames(ocp v1alpha1.ContainerCluster) []string {
	var names []string
	for _, host := range ocp.Spec.Hosts {
		if host.Role == v1alpha1.NodeRoleInfra || len(host.Labels) > 0 || len(host.Taints) > 0 {
			names = append(names, host.Hostname)
		}
	}
	return names
}

func waitNodesRegistered(ctx context.Context, runner extensionoc.OCRunner, kubeconfig, cluster string, names []string, attempts int, interval time.Duration) error {
	for _, name := range names {
		var lastErr error
		for attempt := 0; attempt < attempts; attempt++ {
			if _, err := runner.Run(ctx, kubeconfig, []string{"get", "node", name, "-o", "name"}, nil); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
			if attempt == attempts-1 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
		if lastErr != nil {
			return fmt.Errorf("node config task: host %q is not a registered Node in cluster %q (the kubelet has not joined, or the authored hostname does not match the registered node name): %w", name, cluster, lastErr)
		}
	}
	return nil
}
