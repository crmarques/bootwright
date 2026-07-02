// Package nmstate implements the single deterministic NMState
// template+override merge that both rendering and validation rely on. The merge
// rules are normative in specs/state-model.md ("The merge is the same for
// rendering and validation"); keeping one implementation here is what makes that
// promise hold.
package nmstate

import (
	"fmt"
	"sort"

	"dario.cat/mergo"
)

// InterfaceAddress is a resolved static install address to inject into the
// effective config: the owning kinds resolve the address ref to an IP before
// calling EffectiveConfig so this package stays free of schema types.
type InterfaceAddress struct {
	Interface    string
	Family       string
	IP           string
	PrefixLength int
}

// EffectiveConfig clones the NetworkConfig template, merges the per-machine
// overrides into it, then injects resolved interface addresses. Both render and
// validate call this so they reason about the identical effective config.
func EffectiveConfig(template, overrides map[string]any, addresses []InterfaceAddress) map[string]any {
	out := Clone(template)
	if len(overrides) > 0 {
		// Merge only errors when mergo's top-level contract is violated (a
		// non-pointer dst or mismatched dst/src types); this package always feeds
		// it a *map[string]any dst and a map[string]any src of identical type, so
		// the error is unreachable for these inputs (nested scalar/map/slice
		// conflicts overwrite under WithOverride without erroring). EffectiveConfig
		// cannot surface an error through its signature — its render and validate
		// callers pre-check via the validation phase and ShapeErrors — so treat a
		// merge failure as a broken invariant and panic loudly rather than silently
		// dropping the override merge this package exists to protect.
		if err := Merge(out, Clone(overrides)); err != nil {
			panic(fmt.Sprintf("nmstate: EffectiveConfig merge invariant violated: %v", err))
		}
	}
	for _, address := range addresses {
		SetInterfaceAddress(out, address)
	}
	return out
}

// SetInterfaceAddress writes a resolved static address into the named NMState
// interface, creating the interface and family blocks when absent and
// overwriting any address already on that interface family.
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
	familyConfig["address"] = []any{map[string]any{"ip": address.IP, "prefix-length": address.PrefixLength}}
}

// Merge applies patch onto base in place: maps deep-merge, named list entries
// merge by name, all-map lists merge positionally, and the override value wins
// on scalar conflict. List shapes the merge cannot apply are left untouched here
// and reported by ShapeErrors at validation time.
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
	// mergo folds the leftover scalar/unmergeable keys, override wins. This
	// package exists to prevent silent merge drops, so propagate mergo's error
	// instead of discarding it.
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

// ShapeErrors reports per-machine NMState override list shapes that the merge
// cannot apply, naming the owning field instead of letting the override silently
// drop. It tracks exactly the merge's structured-sequence path: a list override
// is checked only where both template and override hold a list at the same key,
// and is rejected unless every override entry is a named map (merge by name) or
// both lists are positional maps (merge by index).
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

// Clone deep-copies a decoded YAML map so callers can mutate the result without
// touching the loaded desired state.
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

// InterfaceHasStaticIP reports whether the named interface carries a static
// ipv4/ipv6 address in the given config (used to reject an install IP authored
// in overrides for an interface that interfaceAddresses already owns).
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
