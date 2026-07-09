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

// nodeRegistrationAttempts/Interval bound the pre-apply wait for a host's Node
// object to appear. A registered node passes on the first probe (no delay); a
// late-joining worker is given ~5 min to register before the task fails.
const (
	nodeRegistrationAttempts = 30
	nodeRegistrationInterval = 10 * time.Second
)

// runOneNodeConfigTask applies a cluster's day-2 node configuration: the infra
// role labels/taints and the infra MachineConfigPool (plus any authored
// labels/taints). It generates the manifests in-process from the desired host
// specs and applies them with server-side `oc apply` against the installed
// cluster's kubeconfig — the same shape as runOneExtensionTask. The
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
	if err := os.MkdirAll(renderDir, 0o700); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	manifestPath := filepath.Join(renderDir, "node-config.yaml")
	if err := os.WriteFile(manifestPath, manifests, 0o600); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	// Assert every host we are about to patch has actually registered as a Node
	// before applying. Server-side apply would otherwise CREATE a phantom Node for
	// an authored-hostname divergence (typo, uppercase, FQDN-vs-short) — landing
	// the infra taint on a non-existent node while the real one keeps scheduling
	// regular workloads. The read probe is quiet (no console writer) so the
	// expected NotFound lines during a late-worker wait do not alarm.
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

// nodeConfigNodeNames returns the host names that get a day-2 Node patch — the
// hosts nodeConfigManifests emits a Node object for (infra role, or authored
// labels/taints). These are the names that must resolve to a registered Node.
func nodeConfigNodeNames(ocp v1alpha1.ContainerCluster) []string {
	var names []string
	for _, host := range ocp.Spec.Hosts {
		if host.Role == v1alpha1.NodeRoleInfra || len(host.Labels) > 0 || len(host.Taints) > 0 {
			names = append(names, host.Hostname)
		}
	}
	return names
}

// waitNodesRegistered blocks until every named host resolves to a Node object,
// retrying up to attempts times (interval between tries) to allow a late-joining
// worker to register. It fails, naming the missing node, rather than let a
// server-side apply create a phantom Node for a hostname that never registered.
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
