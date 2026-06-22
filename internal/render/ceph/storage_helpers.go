package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

type StorageAttachment struct {
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
}

func storageAttachmentsByStorageCluster(state v1alpha1.State) map[string][]StorageAttachment {
	out := map[string][]StorageAttachment{}
	for _, effect := range addoninputs.StorageExportAttachments(state) {
		key := effect.Export.Spec.StorageClusterRef.Name
		out[key] = append(out[key], StorageAttachment{
			Binding: effect.Binding,
			Addon:   effect.Addon,
			Input:   effect.Input,
		})
	}
	return out
}

func StorageAttachmentByName(state v1alpha1.State, clusterName, addonName, inputName string) (StorageAttachment, bool) {
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		if effect.Binding.Spec.ClusterRef.Name != clusterName {
			continue
		}
		if effect.Addon.AddonRef.Name == addonName && effect.Input.Name == inputName {
			return StorageAttachment{Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input}, true
		}
	}
	return StorageAttachment{}, false
}
