package render

import (
	"fmt"
	"sort"

	"dario.cat/mergo"
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func machineNetworkDefinition(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) (v1alpha1.NetworkConfig, bool) {
	if machine.Network.Spec != nil {
		return v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: fmt.Sprintf("%s/%s", ci.Metadata.Name, machine.Name)},
			Spec:     *machine.Network.Spec,
		}, true
	}
	if machine.Network.NetworkConfigRef.Name != "" {
		return findNetworkConfig(state, machine.Network.NetworkConfigRef.Name)
	}
	return v1alpha1.NetworkConfig{}, false
}

func machineNetworkConfigTemplate(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) map[string]any {
	network, ok := machineNetworkDefinition(state, ci, machine)
	if !ok {
		return nil
	}
	out := cloneYAMLMap(network.Spec.Template.NetworkConfig)
	if len(machine.Network.Overrides) > 0 {
		mergeNetworkConfigOverrides(out, machine.Network.Overrides)
	}
	return out
}

func mergeNetworkConfigOverrides(base map[string]any, overrides map[string]any) {
	patch := cloneYAMLMap(overrides)
	mergeStructuredSequences(base, patch)
	_ = mergo.Merge(&base, patch, mergo.WithOverride)
}

func mergeStructuredSequences(base map[string]any, patch map[string]any) {
	for key, patchValue := range patch {
		baseMap, baseIsMap := base[key].(map[string]any)
		patchMap, patchIsMap := patchValue.(map[string]any)
		if baseIsMap && patchIsMap {
			mergeStructuredSequences(baseMap, patchMap)
			continue
		}
		baseSlice, baseIsSlice := base[key].([]any)
		patchSlice, patchIsSlice := patchValue.([]any)
		if !baseIsSlice || !patchIsSlice {
			continue
		}
		merged, ok := mergeStructuredSequence(baseSlice, patchSlice)
		if !ok {
			continue
		}
		base[key] = merged
		delete(patch, key)
	}
}

func mergeStructuredSequence(base []any, patch []any) ([]any, bool) {
	if len(patch) == 0 {
		return cloneYAMLValue(base).([]any), true
	}
	if sequenceUsesName(patch) {
		return mergeNamedSequence(base, patch), true
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		return mergePositionalMapSequence(base, patch), true
	}
	return nil, false
}

func mergeNamedSequence(base []any, patch []any) []any {
	out := cloneYAMLValue(base).([]any)
	index := map[string]map[string]any{}
	for _, item := range out {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name != "" {
			index[name] = entry
		}
	}
	for _, item := range patch {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		if baseEntry, ok := index[name]; ok {
			mergeNetworkConfigOverrides(baseEntry, entry)
			continue
		}
		out = append(out, cloneYAMLMap(entry))
	}
	return out
}

func mergePositionalMapSequence(base []any, patch []any) []any {
	out := cloneYAMLValue(base).([]any)
	for i, item := range patch {
		entry := item.(map[string]any)
		if i < len(out) {
			baseEntry, ok := out[i].(map[string]any)
			if ok {
				mergeNetworkConfigOverrides(baseEntry, entry)
				continue
			}
		}
		out = append(out, cloneYAMLMap(entry))
	}
	return out
}

func sequenceUsesName(items []any) bool {
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return false
		}
		name, _ := entry["name"].(string)
		if name == "" {
			return false
		}
	}
	return true
}

func sequenceUsesMaps(items []any) bool {
	for _, item := range items {
		if _, ok := item.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func networkConfigInterfaceNames(config map[string]any) []string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func networkConfigPrimaryIP(config map[string]any) string {
	if ip := networkConfigInterfaceIP(config, "", "ipv4"); ip != "" {
		return ip
	}
	return networkConfigInterfaceIP(config, "", "ipv6")
}

func networkConfigInterfaceIP(config map[string]any, interfaceName string, family string) string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return ""
	}
	if family == "" {
		family = "ipv4"
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if interfaceName != "" && name != interfaceName {
			continue
		}
		if ip := networkConfigFamilyIP(entry, family); ip != "" {
			return ip
		}
	}
	return ""
}

func networkConfigFamilyIP(entry map[string]any, family string) string {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return ""
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip != "" {
			return ip
		}
	}
	return ""
}
