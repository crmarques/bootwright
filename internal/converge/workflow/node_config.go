package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	renderDir := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID, "rendered")
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	changed := false
	err := withMaterializedClusterKubeconfig(opts.ContextName, opts.ClustersDir, task.Entry.Cluster, func(kubeconfig string) error {
		checker := extensionoc.CommandRunner{LogPath: logPath}
		configured := nodeConfigNodeNames(ocp)
		if err := waitNodesRegistered(ctx, checker, kubeconfig, task.Entry.Cluster, configured, nodeRegistrationAttempts, nodeRegistrationInterval); err != nil {
			return err
		}
		manifests, err := nodeConfigManifests(ocp, liveNodeSet(ctx, checker, kubeconfig, ocp, configured))
		if err != nil {
			return err
		}
		runner := extensionoc.CommandRunner{
			LogPath: logPath,
			Stdout:  stdout,
			Stderr:  stderr,
		}
		if len(manifests) > 0 {
			if err := os.MkdirAll(renderDir, 0o700); err != nil {
				return err
			}
			manifestPath := filepath.Join(renderDir, "node-config.yaml")
			if err := os.WriteFile(manifestPath, manifests, 0o600); err != nil {
				return err
			}
			if _, err := runner.Run(ctx, kubeconfig, []string{"apply", "-f", manifestPath, "--server-side", "--field-manager", "bootwright", "--force-conflicts"}, nil); err != nil {
				return err
			}
			changed = true
		}
		if clusterDeclaresInfraNode(ocp) {
			return nil
		}
		pruned, err := pruneInfraMachineConfigPool(ctx, runner, kubeconfig)
		if err != nil {
			return err
		}
		if pruned {
			changed = true
		}
		return nil
	})
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	if err := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, runID, task, ConvergeSafetyStatusReconciled, time.Now()); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	if !changed {
		return applyTaskResult{id: task.Entry.ID, skipped: true, skippedReason: "no node config to apply"}
	}
	return applyTaskResult{id: task.Entry.ID}
}

func nodeConfigNodeNames(ocp v1alpha1.ContainerCluster) []string {
	var names []string
	for _, host := range ocp.Spec.Nodes {
		if host.Role == v1alpha1.NodeRoleInfra || len(host.Labels) > 0 || len(host.Taints) > 0 {
			names = append(names, host.Name)
		}
	}
	return names
}

func liveNodeSet(ctx context.Context, runner extensionoc.OCRunner, kubeconfig string, ocp v1alpha1.ContainerCluster, configured []string) map[string]bool {
	live := map[string]bool{}
	for _, name := range configured {
		live[name] = true
	}
	for _, host := range ocp.Spec.Nodes {
		if live[host.Name] {
			continue
		}
		if _, err := runner.Run(ctx, kubeconfig, []string{"get", "node", host.Name, "-o", "name"}, nil); err == nil {
			live[host.Name] = true
		}
	}
	return live
}

func pruneInfraMachineConfigPool(ctx context.Context, runner extensionoc.OCRunner, kubeconfig string) (bool, error) {
	selector := v1alpha1.ManagedByLabel + "=" + v1alpha1.ManagedByLabelValue
	out, err := runner.Run(ctx, kubeconfig, []string{"get", "machineconfigpool", "-l", selector, "-o", "name"}, nil)
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	if _, err := runner.Run(ctx, kubeconfig, []string{"delete", "machineconfigpool", "-l", selector, "--ignore-not-found"}, nil); err != nil {
		return false, err
	}
	return true, nil
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
