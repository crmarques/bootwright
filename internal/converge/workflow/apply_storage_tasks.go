package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	storageapply "github.com/crmarques/bootwright/internal/storage"
)

type StorageBindingPlan struct {
	Cluster string
	Binding v1alpha1.StorageClusterBinding
}

func planStorageBindingTasks(state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) []ApplyTask {
	var tasks []ApplyTask
	exportByName := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exportByName[export.Metadata.Name] = export
	}
	for _, binding := range state.StorageClusterBindings {
		export, ok := exportByName[binding.Spec.StorageExportRef.Name]
		if !ok {
			continue
		}
		for _, cluster := range binding.Spec.ClusterSelector.Names {
			deps := []string{}
			if installPhasePlanned {
				deps = append(deps, "wait."+cluster)
			}
			deps = append(deps, storageDepsByCluster[export.Spec.StorageClusterRef.Name]...)
			deps = append(deps, dataFoundationExtensionWaitDeps(state, cluster)...)
			id := "storagebinding." + cluster + "." + binding.Metadata.Name + ".apply"
			bindingPlan := StorageBindingPlan{Cluster: cluster, Binding: binding}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           id,
					Kind:         ApplyTaskKindStorageClusterBindingApply,
					Label:        "storage binding " + cluster + " " + binding.Metadata.Name + " apply",
					Cluster:      cluster,
					Status:       TaskStatusPending,
					Dependencies: deps,
				},
				State:          stategraph.FilterStateToClusters(state, []string{cluster}),
				StorageBinding: &bindingPlan,
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
		for _, extension := range binding.Extensions {
			if extensionProvides(extension.Extension, v1alpha1.ClusterExtensionProvidesDataFoundation) {
				deps = append(deps, "extension."+cluster+"."+extension.Name+".wait")
			}
		}
	}
	return deps
}

func runOneStorageTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) applyTaskResult {
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
		State:      task.State,
		SecretsDir: opts.SecretsDir,
		Asset:      asset,
	})
	return applyTaskResult{id: task.Entry.ID, err: err}
}

func storageAssetFor(assets []render.StorageAsset, name string) render.StorageAsset {
	for _, asset := range assets {
		if asset.StorageClusterName == name {
			return asset
		}
	}
	return render.StorageAsset{}
}
