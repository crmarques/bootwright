package encoding

import (
	"bytes"
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

// YAML serializes an internal effective inspection catalog. It is deliberately
// separate from authored admission and carries no validation-bypass marker.
func YAML(ctx context.Context, catalog api.Catalog) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	for _, object := range objects(catalog) {
		node, err := yamlValue(ctx, object.Value(), envelope(object.Kind()))
		if err != nil {
			return nil, err
		}
		if err := encoder.Encode(node); err != nil {
			return nil, errors.New("effective YAML encoding failed")
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, errors.New("effective YAML encoding failed")
	}
	return out.Bytes(), nil
}

func JSON(ctx context.Context, catalog api.Catalog) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []byte{'['}
	for i, object := range objects(catalog) {
		if i > 0 {
			out = append(out, ',')
		}
		var err error
		out, err = jsonValue(ctx, out, object.Value(), envelope(object.Kind()))
		if err != nil {
			return nil, err
		}
	}
	out = append(out, ']', '\n')
	return out, nil
}

func objects(catalog api.Catalog) []api.Object {
	objects := catalog.Objects()
	slices.SortStableFunc(objects, func(a, b api.Object) int {
		if rank := api.KindIndex(a.Kind()) - api.KindIndex(b.Kind()); rank != 0 {
			return rank
		}
		return strings.Compare(a.Name(), b.Name())
	})
	return objects
}

func envelope(kind api.Kind) *api.Shape {
	return &api.Shape{Type: api.Mapping, Fields: []api.Field{{Name: "apiVersion"}, {Name: "kind"}, {Name: "metadata", Shape: &api.Shape{Type: api.Mapping, Fields: []api.Field{{Name: "name"}, {Name: "labels", Shape: &api.Shape{Open: true}}}}}, {Name: "spec", Shape: api.Schema(kind)}}}
}

func selectedShape(shape *api.Shape, value api.Value) *api.Shape {
	if shape == nil {
		return &api.Shape{}
	}
	for _, alternative := range shape.Alternatives {
		if alternative.Type == value.Type() {
			return alternative
		}
	}
	return shape
}

func fields(value api.Value, shape *api.Shape) []api.FieldValue {
	out := value.Fields()
	ranks := map[string]int{}
	if shape.KindDefaults {
		for i, kind := range api.Kinds() {
			ranks[string(kind)] = i
		}
	} else {
		for i, f := range shape.Fields {
			ranks[f.Name] = i
		}
	}
	slices.SortStableFunc(out, func(a, b api.FieldValue) int {
		ar, aok := ranks[a.Name]
		br, bok := ranks[b.Name]
		if aok && bok {
			return ar - br
		}
		if aok {
			return -1
		}
		if bok {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func childShape(shape *api.Shape, name string) *api.Shape {
	if shape.KindDefaults {
		return api.Schema(api.Kind(name))
	}
	if field, ok := shape.Field(name); ok {
		return field.Shape
	}
	return shape.Element
}

func yamlValue(ctx context.Context, value api.Value, shape *api.Shape) (*yaml.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shape = selectedShape(shape, value)
	node := &yaml.Node{}
	switch value.Type() {
	case api.Mapping:
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		for _, field := range fields(value, shape) {
			if !field.Value.Present() {
				continue
			}
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field.Name}
			if !plainString(field.Name) {
				key.Style = yaml.DoubleQuotedStyle
			}
			child, err := yamlValue(ctx, field.Value, childShape(shape, field.Name))
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, key, child)
		}
	case api.Sequence:
		node.Kind = yaml.SequenceNode
		node.Tag = "!!seq"
		for _, item := range value.Items() {
			child, err := yamlValue(ctx, item, shape.Element)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
	case api.String:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!str"
		node.Value = value.Text()
		if !plainString(value.Text()) {
			node.Style = yaml.DoubleQuotedStyle
		}
	case api.Boolean:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!bool"
		node.Value = strconv.FormatBool(value.Bool())
	case api.Integer:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!int"
		node.Value = value.Text()
	case api.Number:
		if !validNumber(value.Text()) {
			return nil, errors.New("effective number is not finite decimal")
		}
		node.Kind = yaml.ScalarNode
		node.Tag = "!!float"
		node.Value = value.Text()
	default:
		return nil, errors.New("effective representation contains an absent value")
	}
	return node, nil
}

func jsonValue(ctx context.Context, out []byte, value api.Value, shape *api.Shape) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shape = selectedShape(shape, value)
	switch value.Type() {
	case api.Mapping:
		out = append(out, '{')
		count := 0
		for _, field := range fields(value, shape) {
			if !field.Value.Present() {
				continue
			}
			if count > 0 {
				out = append(out, ',')
			}
			count++
			out = appendJSONString(out, field.Name)
			out = append(out, ':')
			var err error
			out, err = jsonValue(ctx, out, field.Value, childShape(shape, field.Name))
			if err != nil {
				return nil, err
			}
		}
		out = append(out, '}')
	case api.Sequence:
		out = append(out, '[')
		for i, item := range value.Items() {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = jsonValue(ctx, out, item, shape.Element)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, ']')
	case api.String:
		out = appendJSONString(out, value.Text())
	case api.Boolean:
		out = strconv.AppendBool(out, value.Bool())
	case api.Integer:
		out = append(out, value.Text()...)
	case api.Number:
		if !validNumber(value.Text()) {
			return nil, errors.New("effective number is not finite decimal")
		}
		out = append(out, value.Text()...)
	default:
		return nil, errors.New("effective representation contains an absent value")
	}
	return out, nil
}

var decimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func validNumber(value string) bool {
	n, err := strconv.ParseFloat(value, 64)
	return decimal.MatchString(value) && err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func plainString(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\n\r\t\x00") || strings.Contains(value, ": ") || strings.Contains(value, " #") {
		return false
	}
	if strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(value[0])) {
		return false
	}
	if value == "true" || value == "false" || value == "null" || value == "~" || decimal.MatchString(value) {
		return false
	}
	return true
}

func appendJSONString(out []byte, value string) []byte {
	const hex = "0123456789abcdef"
	out = append(out, '"')
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if r < 0x20 {
				out = append(out, '\\', 'u', '0', '0', hex[byte(r)>>4], hex[byte(r)&15])
			} else {
				out = utf8.AppendRune(out, r)
			}
		}
	}
	return append(out, '"')
}
