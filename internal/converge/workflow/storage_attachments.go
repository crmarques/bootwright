package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/fs"
	storageapply "github.com/crmarques/bootwright/internal/storage"
	"go.yaml.in/yaml/v3"
)

func runOneStorageAttachmentTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	if task.StorageAttachment == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage attachment task %s has no plan", task.Entry.ID)}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	result, err := render.All(renderDir, opts.ClustersDir, opts.SecretsDir, task.State)
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	asset := storageAttachmentAssetFor(result.StorageAssets, task.StorageAttachment.Binding.Metadata.Name, task.StorageAttachment.Storage.Name, task.StorageAttachment.Cluster)
	if asset.BindingName == "" {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage attachment asset for %s/%s/%s not rendered", task.StorageAttachment.Cluster, task.StorageAttachment.Binding.Metadata.Name, task.StorageAttachment.Storage.Name)}
	}
	if err := writeStorageAttachmentExternalDetails(asset.ExternalClusterDetailsPath, task.State, *task.StorageAttachment, opts.ClustersDir, opts.SecretsDir); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
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

func writeStorageAttachmentExternalDetails(path string, state v1alpha1.State, plan StorageAttachmentPlan, clustersDir string, secretsDir string) error {
	binding := plan.Binding
	storage := plan.Storage
	export, ok := workflowStorageExportByName(state, storage.ExportRef.Name)
	if !ok {
		return nil
	}
	cluster, ok := workflowStorageClusterByName(state, export.Spec.StorageClusterRef.Name)
	if !ok {
		return fmt.Errorf("StorageCluster/%s not found for storage attachment %s/%s", export.Spec.StorageClusterRef.Name, binding.Metadata.Name, storage.Name)
	}
	attachment := render.StorageAttachment{Binding: binding, Storage: storage}
	if storage.DataFoundation.ExternalDetailsRef.Name != "" {
		detailsJSON, err := render.LoadDataFoundationExternalDetailsJSON(state, secretsDir, storage.DataFoundation.ExternalDetailsRef)
		if err != nil {
			return err
		}
		manifest := render.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, storage.DataFoundation.ExternalDetailsRef.Name)
		return writeStorageAttachmentExternalDetailsManifest(path, manifest)
	}
	detailsJSON, found, err := storageapply.LoadDataFoundationAttachmentDetails(clustersDir, plan.Cluster, binding.Metadata.Name, storage.Name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("data foundation external details for storage attachment %s/%s/%s not found; run apply storage for StorageCluster/%s first", plan.Cluster, binding.Metadata.Name, storage.Name, cluster.Metadata.Name)
	}
	manifest := render.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, "")
	return writeStorageAttachmentExternalDetailsManifest(path, manifest)
}

func writeStorageAttachmentExternalDetailsManifest(path string, manifest map[string]any) error {
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal Data Foundation external cluster details: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Data Foundation manifest directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod Data Foundation manifest directory: %w", err)
	}
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write Data Foundation external cluster details: %w", err)
	}
	return nil
}

func storageAttachmentAssetFor(assets []render.StorageAsset, bindingName, storageName, clusterName string) render.StorageAttachmentAsset {
	for _, asset := range assets {
		for _, attachment := range asset.Attachments {
			if attachment.BindingName == bindingName && attachment.StorageName == storageName && attachment.ContainerClusterName == clusterName {
				return attachment
			}
		}
	}
	return render.StorageAttachmentAsset{}
}

func workflowStorageExportByName(state v1alpha1.State, name string) (v1alpha1.StorageExport, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export, true
		}
	}
	return v1alpha1.StorageExport{}, false
}

func workflowStorageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}
