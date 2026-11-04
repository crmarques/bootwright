package lifecycle

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type claimedNames struct {
	names map[string]bool
	paths map[string][]string
}

func (r diffRead) claimedNames() claimedNames {
	claimed := claimedNames{names: map[string]bool{}, paths: map[string][]string{}}
	for _, side := range []diffSide{r.desired, r.applied} {
		for _, identity := range mapKeys(side.paths) {
			path := side.paths[identity]
			if !slices.Contains(claimed.paths[path], identity) {
				claimed.paths[path] = append(claimed.paths[path], identity)
			}
		}
	}
	return claimed
}

func (c claimedNames) place(directory, base, identity string) string {
	if base == "" || base == "." || base == ".." || filepath.Base(base) != base || strings.ContainsAny(base, `/\`) {
		return ""
	}
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			candidate += "-" + strconv.Itoa(n)
		}
		path := filepath.Join(directory, candidate+".yaml")
		if !slices.ContainsFunc(c.paths[path], func(owner string) bool { return owner != identity }) {
			if !slices.Contains(c.paths[path], identity) {
				c.paths[path] = append(c.paths[path], identity)
			}
			return path
		}
	}
}

func mergeFragments(fragments []fragment, shape *api.Shape) (api.Value, []string, []string) {
	merged := api.MapValue()
	var conflicts, notes []string
	for _, source := range fragments {
		notes = append(notes, source.projection.Notes...)
		value, found := merge(merged, canonical(source.projection.Observed, shape), shape, "spec")
		merged = value
		for _, path := range found {
			if !slices.Contains(conflicts, path) {
				conflicts = append(conflicts, path)
			}
		}
	}
	merged = canonical(without(merged, shape, "spec", conflicts), shape)
	if !merged.Present() {
		merged = api.MapValue()
	}
	return merged, conflicts, append(notes, conflictNotes(fragments, shape, conflicts)...)
}

func (r diffRead) projectedSide(kind api.Kind, name string, side RealSide, fragments []fragment) RealSide {
	shape := api.Schema(kind)
	merged, conflicts, notes := mergeFragments(fragments, shape)
	side.Notes = append(side.Notes, notes...)
	applied, _ := r.applied.authored.Find(kind, name)
	effective, _ := r.applied.effective.Find(kind, name)
	authored := canonical(applied.Spec(), shape)
	keep := r.unproven[kind]
	spec := canonical(overlay(authored, canonical(effective.Spec(), shape), merged, shape, keep, false), shape)
	if !spec.Present() {
		spec = api.MapValue()
	}
	object := api.NewObject(kind, name, canonical(applied.Labels(), nil), spec)
	side.Object = &object
	identity := string(kind) + "/" + name
	paths := slices.DeleteFunc(uncompared(authored, merged, shape, "spec", "", keep, false), func(path string) bool {
		return slices.ContainsFunc(conflicts, func(conflict string) bool { return within(path, conflict) }) ||
			slices.ContainsFunc(r.patches[identity], func(patch holderPatch) bool { return patch.field == path })
	})
	if applied.Labels().Len() != 0 {
		paths = append([]string{"metadata.labels"}, paths...)
	}
	slices.SortStableFunc(paths, func(first, second string) int { return pathDepth(first) - pathDepth(second) })
	side.NotCompared = limitPaths(paths)
	return side
}

func (r diffRead) discoveredSide(kind api.Kind, name string, side RealSide, fragments []fragment, names claimedNames) RealSide {
	merged, _, notes := mergeFragments(fragments, api.Schema(kind))
	side.State = sidePresent
	side.Notes = append(side.Notes, notes...)
	object := api.NewObject(kind, name, api.Value{}, merged)
	side.Object = &object
	identity := string(kind) + "/" + name
	side.Path = names.place(filepath.Dir(r.applied.paths[fragments[0].owner]), strings.ToLower(string(kind))+"-"+name, identity)
	return side
}

func pathDepth(path string) int {
	return strings.Count(path, ".") + strings.Count(path, "[")
}

func overlay(authored, effective, observed api.Value, shape *api.Shape, keep, nested bool) api.Value {
	switch {
	case !observed.Present():
		return authored
	case canonical(realized(effective, observed, shape, keep, nested), shape).Equal(effective):
		return authored
	case observed.Type() == api.Mapping && structured(shape):
		var fields []api.FieldValue
		for _, name := range fieldNames(authored, observed) {
			if value := overlay(authored.Get(name), effective.Get(name), observed.Get(name), childShape(shape, name), keep, nested); value.Present() {
				fields = append(fields, api.FieldValue{Name: name, Value: value})
			}
		}
		return api.MapValue(fields...)
	case observed.Type() == api.Sequence && nameKey(shape) != "":
		key := nameKey(shape)
		applied, effectiveItems, observedItems := itemsByKey(authored, key), itemsByKey(effective, key), itemsByKey(observed, key)
		var items []api.Value
		for _, item := range authored.Items() {
			if _, found := observedItems[item.Get(key).Text()]; !found && (keep || !nested) {
				items = append(items, item)
			}
		}
		for _, item := range observed.Items() {
			name := item.Get(key)
			counterpart, declared := applied[name.Text()]
			if !declared {
				counterpart = api.MapValue(api.FieldValue{Name: key, Value: name})
			}
			items = append(items, overlay(counterpart, effectiveItems[name.Text()], item, shape.Element, keep, true))
		}
		return api.ListValue(items...)
	}
	return observed
}

func realized(effective, observed api.Value, shape *api.Shape, keep, nested bool) api.Value {
	switch {
	case !observed.Present():
		return effective
	case observed.Type() == api.Mapping && structured(shape):
		var fields []api.FieldValue
		for _, name := range fieldNames(effective, observed) {
			if value := realized(effective.Get(name), observed.Get(name), childShape(shape, name), keep, nested); value.Present() {
				fields = append(fields, api.FieldValue{Name: name, Value: value})
			}
		}
		return api.MapValue(fields...)
	case observed.Type() == api.Sequence && nameKey(shape) != "":
		key := nameKey(shape)
		effectiveItems, observedItems := itemsByKey(effective, key), itemsByKey(observed, key)
		var items []api.Value
		for _, item := range effective.Items() {
			if _, found := observedItems[item.Get(key).Text()]; !found && (keep || !nested) {
				items = append(items, item)
			}
		}
		for _, item := range observed.Items() {
			items = append(items, realized(effectiveItems[item.Get(key).Text()], item, shape.Element, keep, true))
		}
		return api.ListValue(items...)
	}
	return observed
}

func structured(shape *api.Shape) bool {
	return shape != nil && !freeMap(shape)
}

func uncompared(authored, observed api.Value, shape *api.Shape, path, key string, keep, nested bool) []string {
	switch {
	case !authored.Present():
		return nil
	case !observed.Present():
		return leaves(authored, shape, path, key)
	case authored.Type() == api.Mapping && observed.Type() == api.Mapping && structured(shape):
		var paths []string
		for _, field := range authored.Fields() {
			if field.Name != key {
				paths = append(paths, uncompared(field.Value, observed.Get(field.Name), childShape(shape, field.Name), joinPath(path, field.Name), "", keep, nested)...)
			}
		}
		return paths
	case authored.Type() == api.Sequence && observed.Type() == api.Sequence && nameKey(shape) != "":
		itemKey := nameKey(shape)
		counterparts := itemsByKey(observed, itemKey)
		var paths []string
		for _, item := range authored.Items() {
			name := item.Get(itemKey).Text()
			if counterpart, found := counterparts[name]; found {
				paths = append(paths, uncompared(item, counterpart, shape.Element, itemPath(path, name), itemKey, keep, true)...)
			} else if keep || !nested {
				paths = append(paths, leaves(item, shape.Element, itemPath(path, name), itemKey)...)
			}
		}
		return paths
	}
	return nil
}

func leaves(value api.Value, shape *api.Shape, path, key string) []string {
	switch {
	case value.Type() == api.Mapping && structured(shape):
		var paths []string
		for _, field := range value.Fields() {
			if field.Name != key {
				paths = append(paths, leaves(field.Value, childShape(shape, field.Name), joinPath(path, field.Name), "")...)
			}
		}
		if len(paths) != 0 {
			return paths
		}
	case value.Type() == api.Sequence && nameKey(shape) != "" && value.Len() != 0:
		var paths []string
		for _, item := range value.Items() {
			paths = append(paths, leaves(item, shape.Element, itemPath(path, item.Get(nameKey(shape)).Text()), nameKey(shape))...)
		}
		return paths
	}
	return []string{path}
}
