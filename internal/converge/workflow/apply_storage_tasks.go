package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	storageapply "github.com/crmarques/bootwright/internal/storage"
)

type StorageAttachmentPlan struct {
	Cluster string
	Binding v1alpha1.ClusterAddonBinding
	Storage v1alpha1.ClusterAddonBindingStorage
}

func planStorageAttachmentTasks(state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) []ApplyTask {
	var tasks []ApplyTask
	exportByName := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exportByName[export.Metadata.Name] = export
	}
	for _, binding := range state.ClusterAddonBindings {
		cluster := binding.Spec.ClusterRef.Name
		for _, storage := range binding.Spec.Storage {
			export, ok := exportByName[storage.ExportRef.Name]
			if !ok {
				continue
			}
			deps := []string{}
			if installPhasePlanned {
				deps = append(deps, "wait."+cluster)
			}
			deps = append(deps, storageDepsByCluster[export.Spec.StorageClusterRef.Name]...)
			deps = append(deps, dataFoundationExtensionWaitDeps(state, cluster)...)
			id := "storageattachment." + cluster + "." + binding.Metadata.Name + "." + storage.Name + ".apply"
			attachmentPlan := StorageAttachmentPlan{Cluster: cluster, Binding: binding, Storage: storage}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           id,
					Kind:         ApplyTaskKindStorageAttachmentApply,
					Label:        "storage attachment " + cluster + " " + binding.Metadata.Name + "/" + storage.Name + " apply",
					Cluster:      cluster,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: deps,
				},
				State:             stategraph.FilterStateToClusters(state, []string{cluster}),
				StorageAttachment: &attachmentPlan,
			})
		}
	}
	return tasks
}

func dataFoundationExtensionWaitDeps(state v1alpha1.State, cluster string) []string {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil
	}
	var deps []string
	for _, binding := range plans {
		if binding.Cluster != cluster {
			continue
		}
		for _, extension := range binding.Addons {
			if extensionProvides(extension.Extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
				deps = append(deps, "addon."+cluster+"."+extension.Name+".wait")
			}
		}
	}
	return deps
}

func runOneStorageTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
	taskLog, closeTaskLog, err := taskLogWriter(TaskLogPath(runsDir, runID, task.Entry.ID))
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	defer closeTaskLog()
	stdout = io.MultiWriter(stdout, taskLog)
	stderr = io.MultiWriter(stderr, taskLog)
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	result, err := render.All(renderDir, opts.ClustersDir, opts.SecretsDir, task.State)
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	asset := storageAssetFor(result.StorageAssets, strings.TrimPrefix(task.Entry.ID, "storage."))
	if asset.StorageClusterName == "" {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage asset for %s not rendered", task.Entry.ID)}
	}
	err = storageapply.ApplyCeph(ctx, stdout, stderr, nil, storageapply.ApplyOptions{
		State:       task.State,
		ClustersDir: opts.ClustersDir,
		SecretsDir:  opts.SecretsDir,
		Asset:       asset,
	})
	return applyTaskResult{id: task.Entry.ID, err: err}
}

func storageTaskState(state v1alpha1.State, name string) v1alpha1.State {
	filtered := stategraph.FilterStateToStorageClusters(state, []string{name})
	filtered.ContainerClusters = nil
	return filtered
}

func storageAssetFor(assets []render.StorageAsset, name string) render.StorageAsset {
	for _, asset := range assets {
		if asset.StorageClusterName == name {
			return asset
		}
	}
	return render.StorageAsset{}
}

func storageClusterManaged(cluster v1alpha1.StorageCluster) bool {
	return cluster.Spec.Management == "" || cluster.Spec.Management == v1alpha1.StorageClusterManagementManaged
}
