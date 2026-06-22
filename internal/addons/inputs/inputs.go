package inputs

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

type EffectiveAddon struct {
	Binding   v1alpha1.ClusterAddonBinding
	Addon     v1alpha1.ClusterAddonBindingAddon
	Extension v1alpha1.ClusterAddon
}

type EffectBinding struct {
	Binding   v1alpha1.ClusterAddonBinding
	Addon     v1alpha1.ClusterAddonBindingAddon
	Input     v1alpha1.ClusterAddonBindingInput
	Extension v1alpha1.ClusterAddon
	Effect    v1alpha1.ClusterAddonInputEffect
}

func EffectiveBindingAddons(state v1alpha1.State, binding v1alpha1.ClusterAddonBinding) []v1alpha1.ClusterAddonBindingAddon {
	sets := profileIndex(state.ClusterAddonProfiles)
	var expanded []v1alpha1.ClusterAddonBindingAddon
	positions := map[string]int{}
	add := func(item v1alpha1.ClusterAddonBindingAddon) {
		if item.AddonRef.Name == "" {
			expanded = append(expanded, item)
			return
		}
		if index, ok := positions[item.AddonRef.Name]; ok {
			expanded[index].Inputs = append(expanded[index].Inputs, item.Inputs...)
			return
		}
		positions[item.AddonRef.Name] = len(expanded)
		expanded = append(expanded, item)
	}
	var visitProfile func(string, map[string]bool)
	visitProfile = func(name string, stack map[string]bool) {
		if stack[name] {
			return
		}
		profile, ok := sets[name]
		if !ok {
			return
		}
		nextStack := map[string]bool{}
		for key, value := range stack {
			nextStack[key] = value
		}
		nextStack[name] = true
		for _, ref := range profile.Spec.ProfileRefs {
			visitProfile(ref.Name, nextStack)
		}
		for _, ref := range profile.Spec.AddonRefs {
			add(v1alpha1.ClusterAddonBindingAddon{AddonRef: ref})
		}
	}
	for _, ref := range binding.Spec.AddonProfileRefs {
		visitProfile(ref.Name, map[string]bool{})
	}
	for _, item := range binding.Spec.Addons {
		add(item)
	}
	return expanded
}

func EffectiveAddons(state v1alpha1.State) []EffectiveAddon {
	addons := addonIndex(state.ClusterAddons)
	var out []EffectiveAddon
	for _, binding := range state.ClusterAddonBindings {
		for _, item := range EffectiveBindingAddons(state, binding) {
			extension, ok := addons[item.AddonRef.Name]
			if !ok {
				continue
			}
			out = append(out, EffectiveAddon{
				Binding:   binding,
				Addon:     item,
				Extension: extension,
			})
		}
	}
	return out
}

func EffectBindings(state v1alpha1.State, effectType, provider string) []EffectBinding {
	var out []EffectBinding
	for _, effective := range EffectiveAddons(state) {
		accepted := acceptedInputIndex(effective.Extension)
		for _, input := range effective.Addon.Inputs {
			accept, ok := accepted[input.Name]
			if !ok {
				continue
			}
			for _, effect := range accept.Effects {
				if effect.Type != effectType {
					continue
				}
				if provider != "" && effect.Provider != provider {
					continue
				}
				out = append(out, EffectBinding{
					Binding:   effective.Binding,
					Addon:     effective.Addon,
					Input:     input,
					Extension: effective.Extension,
					Effect:    effect,
				})
			}
		}
	}
	return out
}

// StorageExportAttachment is one DataFoundation storage-export attachment
// resolved to its target StorageExport. It carries the effect binding's
// addon/input identity and the StorageExport its exportRef input names.
type StorageExportAttachment struct {
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
	Export  v1alpha1.StorageExport
}

// StorageExportAttachments is the single traversal of the dataFoundation
// storageExportAttachment effect bindings, resolving each binding's exportRef
// input to its StorageExport. It is the one owner of the "walk the attachment
// bindings and resolve the export" pattern; callers that need a subset filter
// on the returned Export rather than re-walking the bindings.
func StorageExportAttachments(state v1alpha1.State) []StorageExportAttachment {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exports[export.Metadata.Name] = export
	}
	var out []StorageExportAttachment
	for _, effect := range EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		export, ok := exports[exportRef.Name]
		if !ok {
			continue
		}
		out = append(out, StorageExportAttachment{Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input, Export: export})
	}
	return out
}

func LocalObjectReferenceValue(values map[string]any, field string) v1alpha1.LocalObjectReference {
	return v1alpha1.LocalObjectReference{Name: namedValue(values, field)}
}

func SecretRefValue(values map[string]any, field string) v1alpha1.SecretRef {
	return v1alpha1.SecretRef{Name: namedValue(values, field)}
}

func namedValue(values map[string]any, field string) string {
	// Reference-typed input values are plain name strings, like every other
	// *Ref in the API.
	name, _ := values[field].(string)
	return name
}

func profileIndex(items []v1alpha1.ClusterAddonProfile) map[string]v1alpha1.ClusterAddonProfile {
	out := map[string]v1alpha1.ClusterAddonProfile{}
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func addonIndex(items []v1alpha1.ClusterAddon) map[string]v1alpha1.ClusterAddon {
	out := map[string]v1alpha1.ClusterAddon{}
	for _, item := range items {
		out[item.Metadata.Name] = item
	}
	return out
}

func acceptedInputIndex(extension v1alpha1.ClusterAddon) map[string]v1alpha1.ClusterAddonAcceptedInput {
	out := map[string]v1alpha1.ClusterAddonAcceptedInput{}
	for _, item := range extension.Spec.Accepts.Inputs {
		out[item.Name] = item
	}
	return out
}
