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

func StorageConsumersByCluster(state v1alpha1.State) map[string][]string {
	exportCluster := map[string]string{}
	for _, export := range state.StorageExports {
		exportCluster[export.Metadata.Name] = export.Spec.StorageClusterRef.Name
	}
	consumers := map[string]map[string]bool{}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input).Name
		storageCluster := exportCluster[exportRef]
		if storageCluster == "" {
			continue
		}
		consumer := effect.Binding.Spec.ClusterRef.Name
		if consumer == "" {
			continue
		}
		if consumers[storageCluster] == nil {
			consumers[storageCluster] = map[string]bool{}
		}
		consumers[storageCluster][consumer] = true
	}
	out := make(map[string][]string, len(consumers))
	for storageCluster, set := range consumers {
		clusters := make([]string, 0, len(set))
		for name := range set {
			clusters = append(clusters, name)
		}
		sort.Strings(clusters)
		out[storageCluster] = clusters
	}
	return out
}

func StorageConsumerDestroyConflicts(state v1alpha1.State, selectedStorage, selectedContainer []string) []StorageConsumerConflict {
	destroying := nameSet(selectedStorage)
	if len(destroying) == 0 {
		return nil
	}
	inScope := nameSet(selectedContainer)
	var conflicts []StorageConsumerConflict
	for storageCluster, all := range StorageConsumersByCluster(state) {
		if !destroying[storageCluster] {
			continue
		}
		var clusters []string
		for _, consumer := range all {
			if inScope[consumer] {
				continue
			}
			clusters = append(clusters, consumer)
		}
		if len(clusters) == 0 {
			continue
		}
		conflicts = append(conflicts, StorageConsumerConflict{StorageCluster: storageCluster, ConsumingClusters: clusters})
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		return conflicts[i].StorageCluster < conflicts[j].StorageCluster
	})
	return conflicts
}
