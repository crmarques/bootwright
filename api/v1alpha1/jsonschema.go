package v1alpha1

import (
	"bytes"
	"encoding/json"
	"strings"
)

const JSONSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

func JSONSchema() ([]byte, error) {
	kinds := Kinds()
	choices := make([]any, 0, len(kinds))
	definitions := map[string]any{}
	for _, kind := range kinds {
		choices = append(choices, map[string]any{"$ref": "#/$defs/" + string(kind)})
		definitions[string(kind)] = objectSchema(kind)
	}
	document := map[string]any{
		"$schema":     JSONSchemaDialect,
		"$id":         "https://bootwright.io/schemas/" + strings.TrimPrefix(APIVersion, "bootwright.io/") + ".json",
		"title":       "Bootwright " + APIVersion + " desired state (advisory)",
		"description": "An advisory export; bootwright validate is the authority on admission.",
		"oneOf":       choices,
		"$defs":       definitions,
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func objectSchema(kind Kind) map[string]any {
	metadata := map[string]any{
		"type":                 "object",
		"required":             []string{"name"},
		"additionalProperties": false,
		"properties": map[string]any{
			"name":   map[string]any{"type": "string", "minLength": 1, "maxLength": 63, "pattern": "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"},
			"labels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		},
	}
	return map[string]any{
		"type":                 "object",
		"required":             []string{"apiVersion", "kind", "metadata", "spec"},
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": map[string]any{"const": APIVersion},
			"kind":       map[string]any{"const": string(kind)},
			"metadata":   metadata,
			"spec":       shapeSchema(Schema(kind), false, false, 0),
		},
	}
}

func shapeSchema(shape *Shape, partial, require bool, depth int) map[string]any {
	if shape == nil || depth > 48 {
		return map[string]any{}
	}
	if len(shape.Alternatives) > 0 {
		alternatives := make([]any, 0, len(shape.Alternatives))
		for _, alternative := range shape.Alternatives {
			alternatives = append(alternatives, shapeSchema(alternative, partial, require, depth+1))
		}
		return map[string]any{"anyOf": alternatives}
	}
	schema := map[string]any{}
	switch shape.Type {
	case String:
		schema["type"] = "string"
		if len(shape.Enums) > 0 {
			schema["enum"] = shape.Enums
		}
		if shape.MinLength > 0 {
			schema["minLength"] = shape.MinLength
		}
	case Integer, Number:
		schema["type"] = map[ValueType]string{Integer: "integer", Number: "number"}[shape.Type]
		if shape.Minimum != "" {
			schema["minimum"] = json.Number(shape.Minimum)
		}
		if shape.Maximum != "" {
			schema["maximum"] = json.Number(shape.Maximum)
		}
	case Boolean:
		schema["type"] = "boolean"
	case Sequence:
		schema["type"] = "array"
		if shape.Element != nil {
			schema["items"] = shapeSchema(shape.Element, partial, !partial, depth+1)
		}
		if shape.Unique {
			schema["uniqueItems"] = true
		}
		if shape.MinLength > 0 {
			schema["minItems"] = shape.MinLength
		}
	case Mapping:
		schema["type"] = "object"
		switch {
		case shape.KindDefaults:
			defaults := map[string]any{}
			for _, kind := range Kinds() {
				defaults[string(kind)] = shapeSchema(withoutKindDefaults(Schema(kind)), true, false, depth+1)
			}
			schema["properties"] = defaults
			schema["additionalProperties"] = false
		case shape.Open:
			if shape.Element != nil {
				schema["additionalProperties"] = shapeSchema(shape.Element, partial, !partial, depth+1)
			}
		default:
			properties := map[string]any{}
			required := []string{}
			for _, field := range shape.Fields {
				properties[field.Name] = shapeSchema(field.Shape, partial, require, depth+1)
				if field.Required && require {
					required = append(required, field.Name)
				}
			}
			schema["properties"] = properties
			schema["additionalProperties"] = false
			if len(required) > 0 {
				schema["required"] = required
			}
		}
	}
	return schema
}

func withoutKindDefaults(shape *Shape) *Shape {
	if shape == nil {
		return nil
	}
	copied := *shape
	copied.Fields = nil
	for _, field := range shape.Fields {
		if field.Shape == nil || !field.Shape.KindDefaults {
			copied.Fields = append(copied.Fields, field)
		}
	}
	return &copied
}
