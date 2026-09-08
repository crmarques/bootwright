package compilation

import (
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A normalizer may sort a named list while keeping the authored source intact.
// Retain only moved list prefixes; a diagnostic's effective field still points
// to the original element, including when the entire list came from defaults.
func (r *objectRecord) recordReordering(before, after api.Value, shape *api.Shape) {
	moved := map[string]string{}
	var visit func(api.Value, api.Value, *api.Shape, string, string)
	visit = func(old, current api.Value, schema *api.Shape, oldPath, currentPath string) {
		if schema == nil || !old.Present() || !current.Present() {
			return
		}
		schema = shapeForValue(schema, current)
		if schema.Open || schema.KindDefaults {
			return
		}
		switch current.Type() {
		case api.Mapping:
			if old.Type() != api.Mapping {
				return
			}
			for _, field := range schema.Fields {
				visit(old.Get(field.Name), current.Get(field.Name), field.Shape, oldPath+"."+field.Name, currentPath+"."+field.Name)
			}
		case api.Sequence:
			if old.Type() != api.Sequence {
				return
			}
			previous, next := old.Items(), current.Items()
			positions := map[string]int{}
			identity := func(value api.Value) string {
				if schema.NameKey != "" {
					return value.Get(schema.NameKey).Text()
				}
				if schema.Unique && value.Type() == api.String {
					return value.Text()
				}
				return ""
			}
			for i, value := range previous {
				key := identity(value)
				if key == "" {
					continue
				}
				if _, exists := positions[key]; exists {
					positions[key] = -1
				} else {
					positions[key] = i
				}
			}
			for i, value := range next {
				at := i
				if key := identity(value); key != "" {
					index, exists := positions[key]
					if !exists || index < 0 {
						continue
					}
					at = index
				}
				if at >= len(previous) {
					continue
				}
				from := oldPath + "[" + strconv.Itoa(at) + "]"
				to := currentPath + "[" + strconv.Itoa(i) + "]"
				if from != to {
					moved[to] = from
				}
				visit(previous[at], value, schema.Element, from, to)
			}
		}
	}
	visit(before, after, shape, "$.spec", "$.spec")
	if len(moved) > 0 {
		r.remappings = append(r.remappings, moved)
	}
}

func (r *objectRecord) sourceField(field string) string {
	for i := len(r.remappings) - 1; i >= 0; i-- {
		prefix := field
		for {
			if previous, ok := r.remappings[i][prefix]; ok {
				field = previous + strings.TrimPrefix(field, prefix)
				break
			}
			at := strings.LastIndexAny(prefix, ".[")
			if at < 0 {
				break
			}
			prefix = prefix[:at]
		}
	}
	return field
}
