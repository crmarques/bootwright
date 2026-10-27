//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

// jsonSchemaViolation checks value against the subset of JSON Schema the
// advisory export uses, returning the first violation's path.
func jsonSchemaViolation(root, schema map[string]any, value any, path string) string {
	if ref, ok := schema["$ref"].(string); ok {
		return jsonSchemaViolation(root, root["$defs"].(map[string]any)[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any), value, path)
	}
	if choices, ok := schema["oneOf"].([]any); ok {
		matched := 0
		for _, choice := range choices {
			if jsonSchemaViolation(root, choice.(map[string]any), value, path) == "" {
				matched++
			}
		}
		if matched != 1 {
			return fmt.Sprintf("%s matches %d of oneOf", path, matched)
		}
	}
	if choices, ok := schema["anyOf"].([]any); ok && !slices.ContainsFunc(choices, func(choice any) bool { return jsonSchemaViolation(root, choice.(map[string]any), value, path) == "" }) {
		return path + " matches no anyOf choice"
	}
	if constant, ok := schema["const"]; ok && value != constant {
		return path + " is not its constant"
	}
	if choices, ok := schema["enum"].([]any); ok && !slices.Contains(choices, value) {
		return path + " is not an enum value"
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return path + " is not an object"
		}
		for _, name := range asStrings(schema["required"]) {
			if _, present := object[name]; !present {
				return path + "." + name + " is required"
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, child := range object {
			if property, declared := properties[name]; declared {
				if violation := jsonSchemaViolation(root, property.(map[string]any), child, path+"."+name); violation != "" {
					return violation
				}
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case bool:
				if !additional {
					return path + "." + name + " is not a declared property"
				}
			case map[string]any:
				if violation := jsonSchemaViolation(root, additional, child, path+"."+name); violation != "" {
					return violation
				}
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return path + " is not an array"
		}
		if minimum, ok := schemaCount(schema["minItems"]); ok && len(items) < minimum {
			return path + " has too few items"
		}
		element, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if element != nil {
				if violation := jsonSchemaViolation(root, element, item, fmt.Sprintf("%s[%d]", path, index)); violation != "" {
					return violation
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return path + " is not a string"
		}
		if minimum, ok := schemaCount(schema["minLength"]); ok && len(text) < minimum {
			return path + " is too short"
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return path + " is not a boolean"
		}
	case "integer", "number":
		number, ok := value.(json.Number)
		if !ok {
			return path + " is not a number"
		}
		measured, ok := new(big.Rat).SetString(number.String())
		if !ok || schema["type"] == "integer" && !measured.IsInt() {
			return path + " is not an " + schema["type"].(string)
		}
		for bound, below := range map[string]bool{"minimum": true, "maximum": false} {
			if limit, ok := schema[bound].(json.Number); ok {
				edge, _ := new(big.Rat).SetString(limit.String())
				if compared := measured.Cmp(edge); below && compared < 0 || !below && compared > 0 {
					return path + " is out of bounds"
				}
			}
		}
	}
	return ""
}

func schemaCount(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	count, err := number.Int64()
	return int(count), err == nil
}

func asStrings(value any) []string {
	var names []string
	items, _ := value.([]any)
	for _, item := range items {
		names = append(names, item.(string))
	}
	return names
}

func advisorySchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := api.JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var schema map[string]any
	if err := decoder.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

// documentsOf decodes every YAML document of the sources into JSON values, as
// a JSON Schema validator reads them.
func documentsOf(t *testing.T, sources desiredstate.Sources) map[string]any {
	t.Helper()
	documents := map[string]any{}
	for _, file := range sources.Files {
		decoder := yaml.NewDecoder(bytes.NewReader(file.Bytes()))
		for index := 0; ; index++ {
			var node yaml.Node
			if err := decoder.Decode(&node); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", file.Path(), err)
			}
			var native any
			if err := node.Decode(&native); err != nil {
				t.Fatalf("%s: %v", file.Path(), err)
			}
			encoded, err := json.Marshal(native)
			if err != nil {
				t.Fatalf("%s: %v", file.Path(), err)
			}
			decoded := json.NewDecoder(bytes.NewReader(encoded))
			decoded.UseNumber()
			var value any
			if err := decoded.Decode(&value); err != nil {
				t.Fatal(err)
			}
			documents[fmt.Sprintf("%s#%d", file.Path(), index)] = value
		}
	}
	return documents
}

// Every tracked example validates under the advisory JSON Schema export as it
// does under validate, and a shape fault validate refuses the export refuses
// too (Q110 b).
func TestEveryExampleValidatesAlikeUnderTheAdvisoryJSONSchema(t *testing.T) {
	schema := advisorySchema(t)
	if schema["$schema"] != api.JSONSchemaDialect {
		t.Fatalf("the export names dialect %v", schema["$schema"])
	}
	directories := exampleDirectories(t)
	examples := slices.Sorted(func(yield func(string) bool) {
		for name := range directories {
			if !yield(name) {
				return
			}
		}
	})
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			documents := documentsOf(t, exampleDirectory(t, name))
			if len(documents) < 3 {
				t.Fatalf("the example holds %d documents", len(documents))
			}
			for document, value := range documents {
				if violation := jsonSchemaViolation(schema, schema, value, "$"); violation != "" {
					t.Errorf("%s: %s", document, violation)
				}
			}
		})
	}
	for fault, edit := range map[string]exampleEdit{
		"an unknown field":           {"infra/machines/rhel-01.yaml", "  hardware:\n", "  hardwares: {}\n  hardware:\n"},
		"a value outside an enum":    {"infra/providers/lab-libvirt.yaml", "    machineRef: controller\n", "    machineRef: controller\n    uri: qemu:///session\n"},
		"a missing required field":   {"infra/components/ntp.yaml", "    - name: ip\n      addressRef: ip\n", "    - addressRef: ip\n"},
		"a scalar of the wrong type": {"infra/providers/lab-libvirt.yaml", "      cpu: 1\n", "      cpu: one\n"},
	} {
		t.Run(fault, func(t *testing.T) {
			sources := editedExample(t, "lab-rhel", edit)
			if _, _, err := wireCompiler().Compile(context.Background(), sources); err == nil {
				t.Fatal("validate admitted the fault")
			}
			refused := false
			for _, value := range documentsOf(t, sources) {
				refused = refused || jsonSchemaViolation(schema, schema, value, "$") != ""
			}
			if !refused {
				t.Fatal("the advisory JSON Schema admitted the fault")
			}
		})
	}
}

// A required field a kind default supplies is absent from the authored
// document, so the export requires a record field only inside a list element
// or open-map value, which defaults copy whole; validate stays the stricter.
func TestTheAdvisoryJSONSchemaAdmitsAFieldAKindDefaultSupplies(t *testing.T) {
	schema := advisorySchema(t)
	sources := editedExample(t, "lab-rhel",
		exampleEdit{"infra/components/ntp.yaml", "  management: managed\n", ""},
		exampleEdit{"environment.yaml", "spec:\n", "spec:\n  defaults:\n    NTPServer:\n      management: managed\n"})
	if _, _, err := wireCompiler().Compile(context.Background(), sources); err != nil {
		t.Fatalf("validate refused a field a kind default supplies: %v", err)
	}
	for document, value := range documentsOf(t, sources) {
		if violation := jsonSchemaViolation(schema, schema, value, "$"); violation != "" {
			t.Errorf("%s: %s", document, violation)
		}
	}
}

// The Environment entry of the kind-defaults map omits defaults, which the
// decoder refuses there, so the export neither admits it nor recurses into
// itself.
func TestTheAdvisoryJSONSchemaStaysBounded(t *testing.T) {
	data, err := api.JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	depth, deepest := 0, 0
	for _, c := range data {
		switch c {
		case '{', '[':
			depth++
			deepest = max(deepest, depth)
		case '}', ']':
			depth--
		}
	}
	if len(data) > 1<<20 || deepest > 32 {
		t.Fatalf("the export is %d bytes nested %d deep, want at most 1 MiB and 32", len(data), deepest)
	}
	schema := advisorySchema(t)
	environment := map[string]any{"apiVersion": api.APIVersion, "kind": "Environment", "metadata": map[string]any{"name": "lab"},
		"spec": map[string]any{"defaults": map[string]any{"Environment": map[string]any{"defaults": map[string]any{}}}}}
	if violation := jsonSchemaViolation(schema, schema["$defs"].(map[string]any)["Environment"].(map[string]any), environment, "$"); !strings.Contains(violation, "defaults.Environment.defaults is not a declared property") {
		t.Fatalf("defaults.Environment.defaults gave %q", violation)
	}
}
