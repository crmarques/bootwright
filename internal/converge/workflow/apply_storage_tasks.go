package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/state/graph"
)

type StorageAttachmentPlan struct {
	Cluster string
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
}

func planStorageAttachmentTasks(state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) []ApplyTask {
	var tasks []ApplyTask
	exportByName := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exportByName[export.Metadata.Name] = export
	}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		cluster := effect.Binding.Spec.ClusterRef.Name
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		export, ok := exportByName[exportRef.Name]
		if !ok {
			continue
		}
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+cluster)
		}
		deps = append(deps, storageDepsByCluster[export.Spec.StorageClusterRef.Name]...)
		deps = append(deps, "addon."+cluster+"."+effect.Addon.Name+".wait")
		id := "storageattachment." + cluster + "." + effect.Addon.Name + "." + effect.Input.Name + ".apply"
		attachmentPlan := StorageAttachmentPlan{Cluster: cluster, Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input}
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:           id,
				Kind:         ApplyTaskKindStorageAttachmentApply,
				Label:        "storage attachment " + cluster + " " + effect.Addon.Name + "/" + effect.Input.Name + " apply",
				Cluster:      cluster,
				ClusterKind:  ApplyClusterKindContainer,
				Status:       TaskStatusPending,
				Dependencies: deps,
			},
			State:             stategraph.FilterStateToClusters(state, []string{cluster}),
			StorageAttachment: &attachmentPlan,
		})
	}
	return tasks
}

func storageTaskState(state v1alpha1.State, name string) v1alpha1.State {
	filtered := stategraph.FilterStateToStorageClusters(state, []string{name})
	filtered.ContainerClusters = nil
	return filtered
}

func storageClusterManaged(cluster v1alpha1.StorageCluster) bool {
	return cluster.Spec.Management == "" || cluster.Spec.Management == v1alpha1.StorageClusterManagementManaged
}
