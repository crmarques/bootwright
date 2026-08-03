package workflow

import (
	"encoding/json"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const ConvergeHashSchema = 3

func hashScopedState(state v1alpha1.State) v1alpha1.State {
	state.Environments = environmentsWithoutAuthorizationPolicy(state.Environments)
	roots := state
	roots.Secrets = nil
	roots.Entitlements = nil
	roots.MachineImages = nil
	roots.MachineInstallProfiles = nil
	roots.CustomPlaybooks = nil
	roots.NetworkConfigs = nil

	referenced := map[string]bool{}
	collectStringLeaves(referenced, roots)

	keptSecret := map[string]bool{}
	keptEntitlement := map[string]bool{}
	keptImage := map[string]bool{}
	keptProfile := map[string]bool{}
	keptPlaybook := map[string]bool{}
	keptNetwork := map[string]bool{}
	for {
		grew := false
		for _, obj := range state.Secrets {
			grew = keepReferenced(referenced, keptSecret, obj.Metadata.Name, obj) || grew
		}
		for _, obj := range state.Entitlements {
			grew = keepReferenced(referenced, keptEntitlement, obj.Metadata.Name, obj) || grew
		}
		for _, obj := range state.MachineImages {
			grew = keepReferenced(referenced, keptImage, obj.Metadata.Name, obj) || grew
		}
		for _, obj := range state.MachineInstallProfiles {
			grew = keepReferenced(referenced, keptProfile, obj.Metadata.Name, obj) || grew
		}
		for _, obj := range state.CustomPlaybooks {
			grew = keepReferenced(referenced, keptPlaybook, obj.Metadata.Name, obj) || grew
		}
		for _, obj := range state.NetworkConfigs {
			grew = keepReferenced(referenced, keptNetwork, obj.Metadata.Name, obj) || grew
		}
		if !grew {
			break
		}
	}

	state.Secrets = keepByName(state.Secrets, keptSecret, func(o v1alpha1.Secret) string { return o.Metadata.Name })
	state.Entitlements = keepByName(state.Entitlements, keptEntitlement, func(o v1alpha1.Entitlement) string { return o.Metadata.Name })
	state.MachineImages = keepByName(state.MachineImages, keptImage, func(o v1alpha1.MachineImage) string { return o.Metadata.Name })
	state.MachineInstallProfiles = keepByName(state.MachineInstallProfiles, keptProfile, func(o v1alpha1.MachineInstallProfile) string { return o.Metadata.Name })
	state.CustomPlaybooks = keepByName(state.CustomPlaybooks, keptPlaybook, func(o v1alpha1.CustomPlaybook) string { return o.Metadata.Name })
	state.NetworkConfigs = keepByName(state.NetworkConfigs, keptNetwork, func(o v1alpha1.NetworkConfig) string { return o.Metadata.Name })
	return state
}

func environmentsWithoutAuthorizationPolicy(environments []v1alpha1.Environment) []v1alpha1.Environment {
	if len(environments) == 0 {
		return environments
	}
	out := make([]v1alpha1.Environment, len(environments))
	copy(out, environments)
	for i := range out {
		out[i].Spec.Safety = v1alpha1.EnvironmentSafetySpec{}
	}
	return out
}

func keepReferenced(referenced, kept map[string]bool, name string, obj any) bool {
	if name == "" || kept[name] || !referenced[name] {
		return false
	}
	kept[name] = true
	collectStringLeaves(referenced, obj)
	return true
}

func keepByName[T any](items []T, kept map[string]bool, name func(T) string) []T {
	if len(items) == 0 {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if kept[name(item)] {
			out = append(out, item)
		}
	}
	return out
}

func collectStringLeaves(out map[string]bool, obj any) {
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return
	}
	walkStringLeaves(out, decoded)
}

func walkStringLeaves(out map[string]bool, value any) {
	switch v := value.(type) {
	case string:
		if v != "" {
			out[v] = true
		}
	case []any:
		for _, item := range v {
			walkStringLeaves(out, item)
		}
	case map[string]any:
		for _, item := range v {
			walkStringLeaves(out, item)
		}
	}
}
