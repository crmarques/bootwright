package encoding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

func effectiveMap(values ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(values); i += 2 {
		var value api.Value
		switch v := values[i+1].(type) {
		case api.Value:
			value = v
		case string:
			value = api.StringValue(v)
		case bool:
			value = api.BoolValue(v)
		}
		fields = append(fields, api.FieldValue{Name: values[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func TestInternalDerivedMachineAndContainerValuesRoundTrip(t *testing.T) {
	large := "18446744073709551616000000000000000000000000000001"
	native := effectiveMap("integer", api.IntegerValue(large), "fraction", api.NumberValue("1.25"), "integralNumber", api.NumberValue("1"), "enabled", false, "emptyMap", api.MapValue(), "emptyList", api.ListValue(), "strings", api.StringList("true", "false", "null", "~", "1", "1.25", "2026-01-01", ".nan", "yes", "off", "a: b", "#comment", "literal\\u003c", "<node>", "two\nlines", "é"))
	machine := api.NewObject(api.Machine, "node", api.MapValue(), effectiveMap("os", effectiveMap("provided", false, "installProfileRef", "rhel"), "network", effectiveMap("configRef", "network", "addresses", api.ListValue(effectiveMap("name", "primary", "address", "2001:db8::10/64", "interface", "eth0")), "interfaceBinding", api.ListValue(effectiveMap("nicRef", "eth0", "interfaceName", "eth0"))), "access", effectiveMap("rootLogin", "keep", "ssh", effectiveMap("addressRef", "primary", "port", api.IntegerValue("22"), "user", "bootwright", "auth", effectiveMap("privateKeyRef", "fleet-key")))))
	endpoint := effectiveMap("address", "2001:db8::10", "source", effectiveMap("type", "node"))
	cluster := api.NewObject(api.ContainerCluster, "cluster", api.Value{}, effectiveMap("distribution", effectiveMap("type", "okd", "release", effectiveMap("image", "quay.io/example/release:4.99.1")), "install", effectiveMap("platform", effectiveMap("type", "external", "external", native), "endpoints", effectiveMap("api", endpoint, "api-int", endpoint, "ingress", endpoint)), "nodes", api.ListValue(effectiveMap("name", "master", "fqdn", "master.cluster.example.test", "role", "master", "machineRef", "node"))))
	catalog := api.NewCatalog([]api.Object{cluster, machine})
	yamlData, err := YAML(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := JSON(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	yamlObjects := decodeInspectionYAML(t, yamlData)
	jsonObjects := decodeInspectionJSON(t, jsonData)
	if len(yamlObjects) != 2 || len(jsonObjects) != 2 {
		t.Fatal("effective object count changed")
	}
	for i, value := range yamlObjects {
		if !sameInspectionValue(value, jsonObjects[i]) {
			t.Fatal("YAML/JSON inspection semantics differ")
		}
		original, ok := catalog.Find(api.Kind(value.Get("kind").Text()), value.Get("metadata", "name").Text())
		if !ok || !original.Value().Equal(value) {
			t.Fatal("YAML internal reload changed derived values", original.Identity())
		}
	}
	reloaded := []api.Object{}
	for _, value := range yamlObjects {
		reloaded = append(reloaded, api.NewObject(api.Kind(value.Get("kind").Text()), value.Get("metadata", "name").Text(), value.Get("metadata", "labels"), value.Get("spec")))
	}
	for _, objects := range [][]api.Object{reloaded, {machine, cluster}} {
		nextYAML, err := YAML(context.Background(), api.NewCatalog(objects))
		if err != nil || !bytes.Equal(yamlData, nextYAML) {
			t.Fatal("YAML canonicalization is not idempotent", err)
		}
		nextJSON, err := JSON(context.Background(), api.NewCatalog(objects))
		if err != nil || !bytes.Equal(jsonData, nextJSON) {
			t.Fatal("JSON canonicalization is not idempotent", err)
		}
	}
	if !bytes.Contains(jsonData, []byte(large)) {
		t.Fatal("large native integer was rounded")
	}
	for _, data := range [][]byte{yamlData, jsonData} {
		if bytes.Contains(data, []byte("trustedMode")) || bytes.Contains(data, []byte("effectiveMode")) {
			t.Fatal("internal round trip introduced a trust marker")
		}
	}
}

func TestInspectionRejectsNonDecimalAndNonFiniteNumbers(t *testing.T) {
	for _, number := range []string{"01", ".5", "+1", "NaN", ".inf", "-Inf", "1e999", "0x10"} {
		t.Run(number, func(t *testing.T) {
			object := api.NewObject(api.CustomPlaybook, "reserved", api.Value{}, effectiveMap("extraVars", effectiveMap("number", api.NumberValue(number))))
			catalog := api.NewCatalog([]api.Object{object})
			for _, encode := range []func(context.Context, api.Catalog) ([]byte, error){YAML, JSON} {
				data, err := encode(context.Background(), catalog)
				if err == nil || len(data) != 0 {
					t.Fatal("invalid native number encoded", number)
				}
				if strings.Contains(err.Error(), number) {
					t.Fatal("encoder error echoed rejected native input")
				}
			}
		})
	}
}

// These decoders are test-only inspection reloads. They preserve API scalar
// categories and never call or bypass authored desired-state admission.
func decodeInspectionYAML(t *testing.T, data []byte) []api.Value {
	t.Helper()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	out := []api.Value{}
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, inspectionNode(t, document.Content[0]))
	}
}

func inspectionNode(t *testing.T, node *yaml.Node) api.Value {
	t.Helper()
	switch node.Kind {
	case yaml.MappingNode:
		fields := []api.FieldValue{}
		for i := 0; i < len(node.Content); i += 2 {
			fields = append(fields, api.FieldValue{Name: node.Content[i].Value, Value: inspectionNode(t, node.Content[i+1])})
		}
		return api.MapValue(fields...)
	case yaml.SequenceNode:
		items := []api.Value{}
		for _, child := range node.Content {
			items = append(items, inspectionNode(t, child))
		}
		return api.ListValue(items...)
	case yaml.ScalarNode:
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
	t.Fatalf("unexpected effective YAML node: %d %s", node.Kind, node.Tag)
	return api.Value{}
}

func decodeInspectionJSON(t *testing.T, data []byte) []api.Value {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var values []any
	if err := decoder.Decode(&values); err != nil {
		t.Fatal(err)
	}
	out := []api.Value{}
	for _, value := range values {
		out = append(out, inspectionJSONValue(t, value))
	}
	return out
}

func inspectionJSONValue(t *testing.T, value any) api.Value {
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
			items = append(items, inspectionJSONValue(t, item))
		}
		return api.ListValue(items...)
	case map[string]any:
		keys := []string{}
		for key := range value {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		fields := []api.FieldValue{}
		for _, key := range keys {
			fields = append(fields, api.FieldValue{Name: key, Value: inspectionJSONValue(t, value[key])})
		}
		return api.MapValue(fields...)
	}
	t.Fatalf("unexpected effective JSON value: %T", value)
	return api.Value{}
}

func sameInspectionValue(a, b api.Value) bool {
	if (a.Type() == api.Number || a.Type() == api.Integer) && (b.Type() == api.Number || b.Type() == api.Integer) {
		left, ok := new(big.Rat).SetString(a.Text())
		right, otherOK := new(big.Rat).SetString(b.Text())
		return ok && otherOK && left.Cmp(right) == 0
	}
	if a.Type() != b.Type() || a.Text() != b.Text() || a.Bool() != b.Bool() || a.Len() != b.Len() {
		return false
	}
	for _, field := range a.Fields() {
		if !sameInspectionValue(field.Value, b.Get(field.Name)) {
			return false
		}
	}
	for i, item := range a.Items() {
		if !sameInspectionValue(item, b.Items()[i]) {
			return false
		}
	}
	return true
}
