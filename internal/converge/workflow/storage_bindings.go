package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	extensionoc "github.com/crmarques/bootwright/internal/extensions/oc"
	"github.com/crmarques/bootwright/internal/render"
)

func runOneStorageBindingTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	if task.StorageBinding == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage binding task %s has no binding plan", task.Entry.ID)}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	result, err := render.All(renderDir, opts.ClustersDir, opts.SecretsDir, task.State)
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	asset := storageBindingAssetFor(result.StorageAssets, task.StorageBinding.Binding.Metadata.Name, task.StorageBinding.Cluster)
	if asset.BindingName == "" {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage binding asset for %s/%s not rendered", task.StorageBinding.Cluster, task.StorageBinding.Binding.Metadata.Name)}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	runner := extensionoc.CommandRunner{
		LogPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	for _, path := range []string{asset.ExternalClusterDetailsPath, asset.StorageClusterPath, asset.StorageSystemPath} {
		if _, err := runner.Run(ctx, kubeconfig, []string{"apply", "-f", path, "--server-side", "--field-manager", "bootwright"}, nil); err != nil {
			return applyTaskResult{id: task.Entry.ID, err: err}
		}
	}
	return applyTaskResult{id: task.Entry.ID}
}

func storageBindingAssetFor(assets []render.StorageAsset, bindingName, clusterName string) render.StorageBindingAsset {
	for _, asset := range assets {
		for _, binding := range asset.Bindings {
			if binding.BindingName == bindingName && binding.ContainerClusterName == clusterName {
				return binding
			}
		}
	}
	return render.StorageBindingAsset{}
}
