package stategraph

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

type StorageConsumerConflict struct {
	StorageCluster    string
	ConsumingClusters []string
}

func StorageConsumerDestroyConflicts(state v1alpha1.State, selectedStorage, selectedContainer []string) []StorageConsumerConflict {
	destroying := nameSet(selectedStorage)
	if len(destroying) == 0 {
		return nil
	}
	inScope := nameSet(selectedContainer)
	exportCluster := map[string]string{}
	for _, export := range state.StorageExports {
		exportCluster[export.Metadata.Name] = export.Spec.StorageClusterRef.Name
	}
	consumers := map[string]map[string]bool{}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input).Name
		storageCluster := exportCluster[exportRef]
		if storageCluster == "" || !destroying[storageCluster] {
			continue
		}
		consumer := effect.Binding.Spec.ClusterRef.Name
		if consumer == "" || inScope[consumer] {
			continue
		}
		if consumers[storageCluster] == nil {
			consumers[storageCluster] = map[string]bool{}
		}
		consumers[storageCluster][consumer] = true
	}
	var conflicts []StorageConsumerConflict
	for storageCluster, set := range consumers {
		clusters := make([]string, 0, len(set))
		for name := range set {
			clusters = append(clusters, name)
		}
		sort.Strings(clusters)
		conflicts = append(conflicts, StorageConsumerConflict{StorageCluster: storageCluster, ConsumingClusters: clusters})
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		return conflicts[i].StorageCluster < conflicts[j].StorageCluster
	})
	return conflicts
}
