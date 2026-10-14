package lifecycle

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func childShape(shape *api.Shape, name string) *api.Shape {
	if shape == nil {
		return nil
	}
	if field, found := shape.Field(name); found {
		return field.Shape
	}
	for _, alternative := range shape.Alternatives {
		if alternative == nil {
			continue
		}
		if field, found := alternative.Field(name); found {
			return field.Shape
		}
	}
	return nil
}

func elementShape(shape *api.Shape) *api.Shape {
	if shape == nil {
		return nil
	}
	return shape.Element
}

func nameKey(shape *api.Shape) string {
	if shape == nil || shape.Type != api.Sequence {
		return ""
	}
	return shape.NameKey
}

func canonical(value api.Value, shape *api.Shape) api.Value {
	switch value.Type() {
	case api.Mapping:
		fields := make([]api.FieldValue, 0, value.Len())
		for _, field := range value.Fields() {
			if field.Value.Present() {
				fields = append(fields, api.FieldValue{Name: field.Name, Value: canonical(field.Value, childShape(shape, field.Name))})
			}
		}
		return api.MapValue(fields...)
	case api.Sequence:
		items := value.Items()
		for index, item := range items {
			items[index] = canonical(item, elementShape(shape))
		}
		if key := nameKey(shape); key != "" {
			slices.SortStableFunc(items, func(first, second api.Value) int {
				return strings.Compare(first.Get(key).Text(), second.Get(key).Text())
			})
		}
		return api.ListValue(items...)
	}
	return value
}

func canonicalObject(object api.Object) *api.Object {
	shaped := api.NewObject(object.Kind(), object.Name(), canonical(object.Labels(), nil), canonical(object.Spec(), api.Schema(object.Kind())))
	return &shaped
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func itemPath(prefix, key string) string {
	return prefix + "[" + key + "]"
}

const (
	maxProjectionNotes = 8
	maxProjectionNote  = 200
	minLentPart        = 8
	maxShownValue      = 40
)

func admissible(paths []ProjectedPath, projection Projection) error {
	identity := string(projection.Kind) + "/" + projection.Name
	switch {
	case projection.Kind == api.Secret || !slices.Contains(api.Kinds(), projection.Kind):
		return failure("lifecycle.state", "its projection names a kind its implementation does not observe: "+string(projection.Kind), "")
	case projection.Name == "":
		return failure("lifecycle.state", "its projection of "+string(projection.Kind)+" names no object", "")
	case projection.Observed.Type() != api.Mapping:
		return failure("lifecycle.state", "its projection of "+identity+" is not a spec mapping", "")
	case len(projection.Notes) > maxProjectionNotes:
		return failure("lifecycle.state", "its projection of "+identity+" carries more than "+strconv.Itoa(maxProjectionNotes)+" notes", "")
	}
	for _, note := range projection.Notes {
		if len(note) > maxProjectionNote || strings.ContainsFunc(note, func(r rune) bool { return r < 0x20 }) {
			return failure("lifecycle.state", "its projection of "+identity+" carries a note longer than "+strconv.Itoa(maxProjectionNote)+" bytes or holding a control character", "")
		}
	}
	for _, path := range claimedPaths(projection.Observed, api.Schema(projection.Kind), "spec") {
		if !slices.Contains(paths, ProjectedPath{Kind: projection.Kind, Path: path}) {
			return failure("lifecycle.state", "its projection claims a field its implementation does not observe: "+path, "")
		}
	}
	return nil
}

func claimedPaths(value api.Value, shape *api.Shape, prefix string) []string {
	switch {
	case !value.Present():
		return nil
	case value.Type() == api.Mapping && shape != nil:
		var paths []string
		for _, field := range value.Fields() {
			paths = append(paths, claimedPaths(field.Value, childShape(shape, field.Name), joinPath(prefix, field.Name))...)
		}
		return paths
	case value.Type() == api.Sequence && nameKey(shape) != "":
		if value.Len() == 0 {
			return []string{prefix + "[]." + nameKey(shape)}
		}
		var paths []string
		for _, item := range value.Items() {
			paths = append(paths, claimedPaths(item, shape.Element, prefix+"[]")...)
		}
		return paths
	}
	return []string{prefix}
}

func refuseLentMaterial(projections []Projection, material map[string]secrets.Material, block, contextName string) error {
	var texts [][]byte
	for _, projection := range projections {
		texts = append(texts, []byte(projection.Name))
		texts = append(texts, valueTexts(projection.Observed)...)
		for _, note := range projection.Notes {
			texts = append(texts, []byte(note))
		}
	}
	for _, name := range mapKeys(material) {
		for _, part := range material[name].Parts() {
			lent, _ := material[name].Part(part)
			carried := len(lent) >= minLentPart && slices.ContainsFunc(texts, func(text []byte) bool { return bytes.Contains(text, lent) })
			clear(lent)
			if carried {
				return failure("lifecycle.state", "a projection of block "+block+" carries Secret material, so diff shows nothing",
					unobservedDiff(contextName)+", and report this defect")
			}
		}
	}
	return nil
}

func valueTexts(value api.Value) [][]byte {
	switch value.Type() {
	case api.Mapping:
		var texts [][]byte
		for _, field := range value.Fields() {
			texts = append(texts, []byte(field.Name))
			texts = append(texts, valueTexts(field.Value)...)
		}
		return texts
	case api.Sequence:
		var texts [][]byte
		for _, item := range value.Items() {
			texts = append(texts, valueTexts(item)...)
		}
		return texts
	}
	return [][]byte{[]byte(value.Text())}
}

func merge(into, fragment api.Value, shape *api.Shape, path string) (api.Value, []string) {
	switch {
	case !into.Present():
		return fragment, nil
	case !fragment.Present():
		return into, nil
	case into.Type() == api.Mapping && fragment.Type() == api.Mapping:
		var fields []api.FieldValue
		var conflicts []string
		for _, name := range fieldNames(into, fragment) {
			value, found := merge(into.Get(name), fragment.Get(name), childShape(shape, name), joinPath(path, name))
			conflicts = append(conflicts, found...)
			if value.Present() {
				fields = append(fields, api.FieldValue{Name: name, Value: value})
			}
		}
		return api.MapValue(fields...), conflicts
	case into.Type() == api.Sequence && fragment.Type() == api.Sequence && nameKey(shape) != "":
		key := nameKey(shape)
		previous, next := itemsByKey(into, key), itemsByKey(fragment, key)
		var items []api.Value
		var conflicts []string
		for _, name := range itemKeys(key, into, fragment) {
			value, found := merge(previous[name], next[name], shape.Element, itemPath(path, name))
			conflicts = append(conflicts, found...)
			if value.Present() {
				items = append(items, value)
			}
		}
		return api.ListValue(items...), conflicts
	case into.Equal(fragment):
		return into, nil
	}
	return api.Value{}, []string{path}
}

func itemKeys(key string, values ...api.Value) []string {
	var keys []string
	for _, value := range values {
		for _, item := range value.Items() {
			if name := item.Get(key).Text(); !slices.Contains(keys, name) {
				keys = append(keys, name)
			}
		}
	}
	return keys
}

func without(value api.Value, shape *api.Shape, path string, conflicts []string) api.Value {
	switch {
	case !value.Present() || slices.Contains(conflicts, path):
		return api.Value{}
	case value.Type() == api.Mapping:
		var fields []api.FieldValue
		for _, field := range value.Fields() {
			if kept := without(field.Value, childShape(shape, field.Name), joinPath(path, field.Name), conflicts); kept.Present() {
				fields = append(fields, api.FieldValue{Name: field.Name, Value: kept})
			}
		}
		return api.MapValue(fields...)
	case value.Type() == api.Sequence && nameKey(shape) != "":
		var items []api.Value
		for _, item := range value.Items() {
			if kept := without(item, shape.Element, itemPath(path, item.Get(nameKey(shape)).Text()), conflicts); kept.Present() {
				items = append(items, kept)
			}
		}
		return api.ListValue(items...)
	}
	return value
}

func prune(declared, observed api.Value, shape *api.Shape, path string, unproven bool) (api.Value, []string) {
	switch {
	case !declared.Present():
		return declared, nil
	case !observed.Present():
		return api.Value{}, []string{path}
	case declared.Type() == api.Mapping && observed.Type() == api.Mapping:
		var fields []api.FieldValue
		var dropped []string
		for _, field := range declared.Fields() {
			value, lost := prune(field.Value, observed.Get(field.Name), childShape(shape, field.Name), joinPath(path, field.Name), unproven)
			dropped = append(dropped, lost...)
			if value.Present() {
				fields = append(fields, api.FieldValue{Name: field.Name, Value: value})
			}
		}
		return api.MapValue(fields...), dropped
	case declared.Type() == api.Sequence && observed.Type() == api.Sequence && nameKey(shape) != "":
		return pruneItems(declared, observed, shape, path, unproven)
	}
	return declared, nil
}

func pruneItems(declared, observed api.Value, shape *api.Shape, path string, unproven bool) (api.Value, []string) {
	key := nameKey(shape)
	counterparts := itemsByKey(observed, key)
	template := api.Value{}
	for _, item := range observed.Items() {
		template = union(template, item, shape.Element)
	}
	var items []api.Value
	var dropped []string
	for _, item := range declared.Items() {
		name := item.Get(key)
		_, found := counterparts[name.Text()]
		if !found && unproven {
			dropped = append(dropped, itemPath(path, name.Text()))
			continue
		}
		against := template
		if !found {
			against = api.MapValue(api.FieldValue{Name: key, Value: name})
		}
		value, lost := prune(item, against, shape.Element, itemPath(path, name.Text()), unproven)
		dropped = append(dropped, lost...)
		if value.Present() {
			items = append(items, value)
		}
	}
	return api.ListValue(items...), dropped
}

func union(first, second api.Value, shape *api.Shape) api.Value {
	switch {
	case !first.Present():
		return second
	case !second.Present():
		return first
	case first.Type() == api.Mapping && second.Type() == api.Mapping:
		var fields []api.FieldValue
		for _, name := range fieldNames(first, second) {
			fields = append(fields, api.FieldValue{Name: name, Value: union(first.Get(name), second.Get(name), childShape(shape, name))})
		}
		return api.MapValue(fields...)
	case first.Type() == api.Sequence && second.Type() == api.Sequence && nameKey(shape) != "":
		key := nameKey(shape)
		previous, next := itemsByKey(first, key), itemsByKey(second, key)
		var items []api.Value
		for _, name := range itemKeys(key, first, second) {
			items = append(items, union(previous[name], next[name], shape.Element))
		}
		return api.ListValue(items...)
	}
	return first
}

func valueAt(value api.Value, shape *api.Shape, prefix, target string) (api.Value, bool) {
	switch {
	case prefix == target:
		return value, value.Present()
	case value.Type() == api.Mapping:
		for _, field := range value.Fields() {
			if path := joinPath(prefix, field.Name); within(target, path) {
				return valueAt(field.Value, childShape(shape, field.Name), path, target)
			}
		}
	case value.Type() == api.Sequence && nameKey(shape) != "":
		for _, item := range value.Items() {
			if path := itemPath(prefix, item.Get(nameKey(shape)).Text()); within(target, path) {
				return valueAt(item, shape.Element, path, target)
			}
		}
	}
	return api.Value{}, false
}

func within(target, path string) bool {
	return target == path || strings.HasPrefix(target, path+".") || strings.HasPrefix(target, path+"[")
}

func shortValue(value api.Value) string {
	text := value.Text()
	switch value.Type() {
	case api.Boolean:
		text = strconv.FormatBool(value.Bool())
	case api.Mapping:
		text = "a mapping"
	case api.Sequence:
		text = "a list"
	}
	if len(text) > maxShownValue {
		text = text[:maxShownValue] + "..."
	}
	return text
}
