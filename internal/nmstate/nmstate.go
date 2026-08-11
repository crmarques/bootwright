package nmstate

import (
	"fmt"
	"sort"

	"dario.cat/mergo"
)

type InterfaceAddress struct {
	Interface    string
	Family       string
	IP           string
	PrefixLength int
}

func EffectiveConfig(template, overrides map[string]any, addresses []InterfaceAddress) map[string]any {
	out := Clone(template)
	if len(overrides) > 0 {
		if err := Merge(out, Clone(overrides)); err != nil {
			panic(fmt.Sprintf("nmstate: EffectiveConfig merge invariant violated: %v", err))
		}
	}
	for _, address := range addresses {
		SetInterfaceAddress(out, address)
	}
	return out
}

func SetInterfaceAddress(config map[string]any, address InterfaceAddress) {
	if address.IP == "" || address.Interface == "" {
		return
	}
	family := address.Family
	if family == "" {
		family = "ipv4"
	}
	interfaces, _ := config["interfaces"].([]any)
	var entry map[string]any
	for _, item := range interfaces {
		if m, ok := item.(map[string]any); ok {
			if name, _ := m["name"].(string); name == address.Interface {
				entry = m
				break
			}
		}
	}
	if entry == nil {
		entry = map[string]any{"name": address.Interface}
		interfaces = append(interfaces, entry)
		config["interfaces"] = interfaces
	}
	familyConfig, _ := entry[family].(map[string]any)
	if familyConfig == nil {
		familyConfig = map[string]any{}
		entry[family] = familyConfig
	}
	familyConfig["enabled"] = true
	familyConfig["address"] = []any{map[string]any{"ip": address.IP, "prefix-length": address.PrefixLength}}
}

func PhysicalInterfaceNames(config map[string]any) []string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		interfaceType, _ := entry["type"].(string)
		if name != "" && !virtualInterfaceType(interfaceType) {
			seen[name] = true
		}
		aggregation, _ := entry["link-aggregation"].(map[string]any)
		for _, port := range stringSequence(aggregation["port"]) {
			seen[port] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func virtualInterfaceType(interfaceType string) bool {
	switch interfaceType {
	case "bond", "vlan", "vxlan", "bridge", "linux-bridge", "ovs-bridge",
		"ovs-interface", "team", "vrf", "dummy", "macvlan", "macvtap", "ipvlan":
		return true
	default:
		return false
	}
}

func stringSequence(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name, _ := item.(string)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func Merge(base, patch map[string]any) error {
	for key, patchValue := range patch {
		baseMap, baseIsMap := base[key].(map[string]any)
		patchMap, patchIsMap := patchValue.(map[string]any)
		if baseIsMap && patchIsMap {
			if err := Merge(baseMap, patchMap); err != nil {
				return err
			}
			continue
		}
		baseSlice, baseIsSlice := base[key].([]any)
		patchSlice, patchIsSlice := patchValue.([]any)
		if !baseIsSlice || !patchIsSlice {
			continue
		}
		merged, ok, err := mergeSequence(baseSlice, patchSlice)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		base[key] = merged
		delete(patch, key)
	}
	return mergo.Merge(&base, patch, mergo.WithOverride)
}

func mergeSequence(base, patch []any) ([]any, bool, error) {
	if len(patch) == 0 {
		return cloneValue(base).([]any), true, nil
	}
	if sequenceUsesName(patch) {
		merged, err := mergeNamedSequence(base, patch)
		return merged, true, err
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		merged, err := mergePositionalSequence(base, patch)
		return merged, true, err
	}
	return nil, false, nil
}

func mergeNamedSequence(base, patch []any) ([]any, error) {
	out := cloneValue(base).([]any)
	index := map[string]map[string]any{}
	for _, item := range out {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := entry["name"].(string); name != "" {
			index[name] = entry
		}
	}
	for _, item := range patch {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		if baseEntry, ok := index[name]; ok {
			if err := Merge(baseEntry, entry); err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, Clone(entry))
	}
	return out, nil
}

func mergePositionalSequence(base, patch []any) ([]any, error) {
	out := cloneValue(base).([]any)
	for i, item := range patch {
		entry := item.(map[string]any)
		if i < len(out) {
			if baseEntry, ok := out[i].(map[string]any); ok {
				if err := Merge(baseEntry, entry); err != nil {
					return nil, err
				}
				continue
			}
		}
		out = append(out, Clone(entry))
	}
	return out, nil
}

func sequenceUsesName(items []any) bool {
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return false
		}
		if name, _ := entry["name"].(string); name == "" {
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

func ShapeErrors(prefix string, base, patch map[string]any) []string {
	if len(patch) == 0 {
		return nil
	}
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var errs []string
	for _, key := range keys {
		path := prefix + "." + key
		patchValue := patch[key]
		if patchMap, ok := patchValue.(map[string]any); ok {
			if baseMap, ok := base[key].(map[string]any); ok {
				errs = append(errs, ShapeErrors(path, baseMap, patchMap)...)
			}
			continue
		}
		patchSlice, patchIsSlice := patchValue.([]any)
		baseSlice, baseIsSlice := base[key].([]any)
		if !patchIsSlice || !baseIsSlice {
			continue
		}
		errs = append(errs, sequenceShapeErrors(path, baseSlice, patchSlice)...)
	}
	return errs
}

func sequenceShapeErrors(path string, base, patch []any) []string {
	if len(patch) == 0 {
		return nil
	}
	if sequenceUsesName(patch) {
		index := map[string]map[string]any{}
		for _, item := range base {
			if entry, ok := item.(map[string]any); ok {
				if name, _ := entry["name"].(string); name != "" {
					index[name] = entry
				}
			}
		}
		var errs []string
		for _, item := range patch {
			entry := item.(map[string]any)
			name, _ := entry["name"].(string)
			baseEntry := index[name]
			if baseEntry == nil {
				baseEntry = map[string]any{}
			}
			errs = append(errs, ShapeErrors(path+"["+name+"]", baseEntry, entry)...)
		}
		return errs
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		var errs []string
		for i, item := range patch {
			entry := item.(map[string]any)
			baseEntry := map[string]any{}
			if i < len(base) {
				if existing, ok := base[i].(map[string]any); ok {
					baseEntry = existing
				}
			}
			errs = append(errs, ShapeErrors(fmt.Sprintf("%s[%d]", path, i), baseEntry, entry)...)
		}
		return errs
	}
	return []string{path + " override list cannot be merged into the NetworkConfig template; entries must be all named maps or a positional list of maps, not scalars or mixed named and unnamed entries"}
}

func Clone(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return Clone(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneValue(item))
		}
		return out
	default:
		return typed
	}
}

func InterfaceHasStaticIP(config map[string]any, iface string) bool {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return false
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := entry["name"].(string); name != iface {
			continue
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			familyConfig, ok := entry[family].(map[string]any)
			if !ok {
				continue
			}
			if addresses, ok := familyConfig["address"].([]any); ok && len(addresses) > 0 {
				return true
			}
		}
	}
	return false
}
