package lifecycle

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"unicode"

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

// freeMap is a mapping whose keys are data, such as labels, handled whole.
func freeMap(shape *api.Shape) bool {
	return shape != nil && shape.Type == api.Mapping && shape.Open && len(shape.Fields) == 0
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
		if shape != nil && len(shape.ComparisonKeys) != 0 {
			items = comparisonItems(items, shape.ComparisonKeys)
		}
		return api.ListValue(items...)
	}
	return value
}

func comparisonItems(items []api.Value, keys []string) []api.Value {
	compare := func(first, second api.Value) int {
		for _, key := range keys {
			if order := strings.Compare(first.Get(key).Text(), second.Get(key).Text()); order != 0 {
				return order
			}
		}
		return 0
	}
	slices.SortStableFunc(items, compare)
	return slices.CompactFunc(items, func(first, second api.Value) bool { return compare(first, second) == 0 })
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

func admissible(paths []ProjectedPath, discovers []api.Kind, applied api.Catalog, projection Projection) error {
	identity := string(projection.Kind) + "/" + projection.Name
	_, declared := applied.Find(projection.Kind, projection.Name)
	holder := projection.Holder
	switch {
	case projection.Kind == api.Secret || !slices.Contains(api.Kinds(), projection.Kind):
		return failure("lifecycle.state", "its projection names a kind its implementation does not observe: "+string(projection.Kind), "")
	case projection.Name == "":
		return failure("lifecycle.state", "its projection of "+string(projection.Kind)+" names no object", "")
	case !api.ValidLexical("name", projection.Name):
		return failure("lifecycle.state", "its projection of "+string(projection.Kind)+" names an object by an invalid name: "+strconv.Quote(projection.Name), "")
	case holder != nil && !api.ValidLexical("name", holder.Name):
		return failure("lifecycle.state", "its projection of "+identity+" names a holder by an invalid name: "+strconv.Quote(holder.Name), "")
	case !declared && !slices.Contains(discovers, projection.Kind):
		return failure("lifecycle.state", "its projection names an object the applied revision does not declare: "+identity, "")
	case holder != nil && !declaredIn(applied, holder.Kind, holder.Name):
		return failure("lifecycle.state", "its projection of "+identity+" names a holder the applied revision does not declare: "+string(holder.Kind)+"/"+holder.Name, "")
	case holder != nil && !referencesKind(holder.Kind, holder.Field, projection.Kind):
		return failure("lifecycle.state", "its projection of "+identity+" names a holder field that does not reference "+string(projection.Kind)+": "+holder.Field, "")
	case projection.Observed.Type() != api.Mapping:
		return failure("lifecycle.state", "its projection of "+identity+" is not a spec mapping", "")
	case len(projection.Notes) > maxProjectionNotes:
		return failure("lifecycle.state", "its projection of "+identity+" carries more than "+strconv.Itoa(maxProjectionNotes)+" notes", "")
	}
	for _, note := range projection.Notes {
		if len(note) > maxProjectionNote || strings.ContainsFunc(note, unicode.IsControl) {
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

func declaredIn(catalog api.Catalog, kind api.Kind, name string) bool {
	_, found := catalog.Find(kind, name)
	return found
}

func claimedPaths(value api.Value, shape *api.Shape, prefix string) []string {
	switch {
	case !value.Present():
		return nil
	case value.Type() == api.Mapping && shape != nil && !freeMap(shape):
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
	if carriesLent(texts, material) {
		return failure("lifecycle.state", "a projection of block "+block+" carries Secret material, so diff shows nothing",
			unobservedDiff(contextName)+", and report this defect")
	}
	return nil
}

func carriesLent(texts [][]byte, material map[string]secrets.Material) bool {
	for _, name := range mapKeys(material) {
		for _, part := range material[name].Parts() {
			lent, _ := material[name].Part(part)
			carried := len(lent) >= minLentPart && slices.ContainsFunc(texts, func(text []byte) bool { return bytes.Contains(text, lent) })
			clear(lent)
			if carried {
				return true
			}
		}
	}
	return false
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
	case into.Type() == api.Mapping && fragment.Type() == api.Mapping && !freeMap(shape):
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
	case value.Type() == api.Mapping && !freeMap(shape):
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
