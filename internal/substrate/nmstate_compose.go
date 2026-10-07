package substrate

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// MergeNative applies the Machine API's NMState override merge contract.
func MergeNative(base, override api.Value) (api.Value, bool) {
	if base.Type() == api.Mapping && override.Type() == api.Mapping {
		result := base
		for _, field := range override.Fields() {
			value := field.Value
			if previous := base.Get(field.Name); previous.Present() {
				var ok bool
				if value, ok = MergeNative(previous, value); !ok {
					return api.Value{}, false
				}
			}
			result = result.With(field.Name, value)
		}
		return result, true
	}
	if base.Type() != api.Sequence || override.Type() != api.Sequence {
		return override, true
	}
	left, right := base.Items(), override.Items()
	style := listMergeStyle(append(slices.Clone(left), right...))
	if style == "invalid" {
		return api.Value{}, false
	}
	result := slices.Clone(left)
	for index, value := range right {
		position := index
		if style == "named" {
			position = namedIndex(result, value.Get("name").Text())
		}
		if position < 0 || position >= len(result) {
			result = append(result, value)
			continue
		}
		merged, ok := MergeNative(result[position], value)
		if !ok {
			return api.Value{}, false
		}
		result[position] = merged
	}
	return api.ListValue(result...), true
}

func listMergeStyle(items []api.Value) string {
	named, unnamed := false, false
	for _, item := range items {
		if item.Type() != api.Mapping {
			return "invalid"
		}
		if item.Get("name").Type() == api.String && item.Get("name").Text() != "" {
			named = true
		} else {
			unnamed = true
		}
	}
	if named && unnamed {
		return "invalid"
	}
	if named {
		return "named"
	}
	return "positional"
}

func namedIndex(values []api.Value, name string) int {
	position := -1
	for index, value := range values {
		if value.Get("name").Text() == name {
			if position != -1 {
				return -1
			}
			position = index
		}
	}
	return position
}
