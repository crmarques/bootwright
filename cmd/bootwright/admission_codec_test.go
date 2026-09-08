package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
)

func TestActualCompilerEffectiveYAMLAndJSONRoundTrips(t *testing.T) {
	state, _ := compileAcceptance(t, expandedExampleSources(t))
	catalog := state.Effective()
	yamlData, err := effectiveencoding.YAML(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := effectiveencoding.JSON(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/effective.yaml", yamlData)})
	if err != nil || len(diagnostics) != 0 {
		t.Fatal(err, diagnostics)
	}
	yamlValues := make([]api.Value, 0, len(documents))
	for _, document := range documents {
		if len(document.Root.Content) != 1 {
			t.Fatal("invalid effective document")
		}
		yamlValues = append(yamlValues, syntaxValue(t, document.Root.Content[0]))
	}
	var raw []any
	decoder := json.NewDecoder(bytes.NewReader(jsonData))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	jsonValues := make([]api.Value, len(raw))
	for i, value := range raw {
		jsonValues[i] = jsonValueForAcceptance(t, value)
	}
	if len(yamlValues) != len(catalog.Objects()) || len(jsonValues) != len(yamlValues) {
		t.Fatal("round trip lost effective objects")
	}
	for i, value := range yamlValues {
		if !equivalentEffectiveValues(value, jsonValues[i]) {
			t.Fatal("YAML/JSON semantics differ", value.Get("kind").Text(), value.Get("metadata", "name").Text(), valueDifference(value, jsonValues[i], "$"))
		}
		original, ok := catalog.Find(api.Kind(value.Get("kind").Text()), value.Get("metadata", "name").Text())
		if !ok || !original.Value().Equal(value) {
			t.Fatal("effective value changed during round trip", original.Identity())
		}
	}
	// The internal inspection reload keeps derived values without calling the
	// authored-input compiler or giving the document a trust-mode marker.
	objects := make([]api.Object, len(yamlValues))
	for i, value := range yamlValues {
		objects[i] = api.NewObject(api.Kind(value.Get("kind").Text()), value.Get("metadata", "name").Text(), value.Get("metadata", "labels"), value.Get("spec"))
	}
	second, err := effectiveencoding.YAML(context.Background(), api.NewCatalog(objects))
	if err != nil || !bytes.Equal(yamlData, second) {
		t.Fatal("effective YAML is not canonically idempotent", err)
	}
	second, err = effectiveencoding.JSON(context.Background(), api.NewCatalog(objects))
	if err != nil || !bytes.Equal(jsonData, second) {
		t.Fatal("effective JSON is not canonically idempotent", err)
	}
}

func equivalentEffectiveValues(a, b api.Value) bool {
	if (a.Type() == api.Number || a.Type() == api.Integer) && (b.Type() == api.Number || b.Type() == api.Integer) {
		left, ok := new(big.Rat).SetString(a.Text())
		right, otherOK := new(big.Rat).SetString(b.Text())
		return ok && otherOK && left.Cmp(right) == 0
	}
	if a.Type() != b.Type() || a.Text() != b.Text() || a.Bool() != b.Bool() || a.Len() != b.Len() {
		return false
	}
	for _, field := range a.Fields() {
		if !equivalentEffectiveValues(field.Value, b.Get(field.Name)) {
			return false
		}
	}
	for i, item := range a.Items() {
		if !equivalentEffectiveValues(item, b.Items()[i]) {
			return false
		}
	}
	return true
}

func valueDifference(a, b api.Value, path string) string {
	if a.Type() != b.Type() || a.Text() != b.Text() || a.Bool() != b.Bool() {
		return path + ": " + a.Text() + " / " + b.Text()
	}
	for _, field := range a.Fields() {
		if !field.Value.Equal(b.Get(field.Name)) {
			return valueDifference(field.Value, b.Get(field.Name), path+"."+field.Name)
		}
	}
	for i, item := range a.Items() {
		if i >= b.Len() {
			return path + " missing element"
		}
		if !item.Equal(b.Items()[i]) {
			return valueDifference(item, b.Items()[i], path+"[]")
		}
	}
	return path
}

func syntaxValue(t *testing.T, node *desiredstate.Node) api.Value {
	t.Helper()
	switch node.Kind {
	case desiredstate.MappingKind:
		fields := []api.FieldValue{}
		for i := 0; i < len(node.Content); i += 2 {
			fields = append(fields, api.FieldValue{Name: node.Content[i].Value, Value: syntaxValue(t, node.Content[i+1])})
		}
		return api.MapValue(fields...)
	case desiredstate.SequenceKind:
		items := []api.Value{}
		for _, child := range node.Content {
			items = append(items, syntaxValue(t, child))
		}
		return api.ListValue(items...)
	case desiredstate.ScalarKind:
		switch node.Tag {
		case "!!str":
			return api.StringValue(node.Value)
		case "!!int":
			return api.IntegerValue(node.Value)
		case "!!float":
			return api.NumberValue(node.Value)
		case "!!bool":
			return api.BoolValue(node.Value == "true")
		}
	}
	t.Fatalf("unsupported effective scalar or node: %d %s", node.Kind, node.Tag)
	return api.Value{}
}

func jsonValueForAcceptance(t *testing.T, value any) api.Value {
	t.Helper()
	switch value := value.(type) {
	case string:
		return api.StringValue(value)
	case bool:
		return api.BoolValue(value)
	case json.Number:
		if strings.ContainsAny(value.String(), ".eE") {
			return api.NumberValue(value.String())
		}
		return api.IntegerValue(value.String())
	case []any:
		items := []api.Value{}
		for _, item := range value {
			items = append(items, jsonValueForAcceptance(t, item))
		}
		return api.ListValue(items...)
	case map[string]any:
		fields := []api.FieldValue{}
		for key, item := range value {
			fields = append(fields, api.FieldValue{Name: key, Value: jsonValueForAcceptance(t, item)})
		}
		return api.MapValue(fields...)
	}
	t.Fatalf("unsupported effective JSON value: %T", value)
	return api.Value{}
}
