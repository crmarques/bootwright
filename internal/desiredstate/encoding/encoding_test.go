package encoding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

func TestCanonicalYAMLAndJSONPreserveInternalEffectiveValues(t *testing.T) {
	env := api.NewObject(api.Environment, "synthetic", api.MapValue(api.FieldValue{Name: "z", Value: api.StringValue("true")}, api.FieldValue{Name: "a", Value: api.StringValue("example")}), api.MapValue(api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "machines", Value: api.StringValue("example.test")}, api.FieldValue{Name: "base", Value: api.StringValue("example.test")})}))
	secret := api.NewObject(api.Secret, "material", api.Value{}, api.MapValue(api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "contextStore", Value: api.MapValue()})}, api.FieldValue{Name: "type", Value: api.StringValue("opaque")}))
	catalog := api.NewCatalog([]api.Object{secret, env})
	got, err := YAML(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	want := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: synthetic\n  labels:\n    a: example\n    z: \"true\"\nspec:\n  domains:\n    base: example.test\n    machines: example.test\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  type: opaque\n  source:\n    contextStore: {}\n"
	if string(got) != want {
		t.Fatalf("canonical YAML:\n%s", got)
	}
	jsonBytes, err := JSON(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := "[{\"apiVersion\":\"bootwright.io/v1alpha1\",\"kind\":\"Environment\",\"metadata\":{\"name\":\"synthetic\",\"labels\":{\"a\":\"example\",\"z\":\"true\"}},\"spec\":{\"domains\":{\"base\":\"example.test\",\"machines\":\"example.test\"}}},{\"apiVersion\":\"bootwright.io/v1alpha1\",\"kind\":\"Secret\",\"metadata\":{\"name\":\"material\"},\"spec\":{\"type\":\"opaque\",\"source\":{\"contextStore\":{}}}}]\n"
	if string(jsonBytes) != wantJSON {
		t.Fatalf("canonical JSON: %s", jsonBytes)
	}
	for _, ordered := range [][]api.Object{{env, secret}, {secret, env}} {
		again, err := YAML(context.Background(), api.NewCatalog(ordered))
		if err != nil || !bytes.Equal(got, again) {
			t.Fatal("input order affected serialization")
		}
	}
	// The inspection codec is tested independently of authored admission; no
	// compiler entrypoint accepts an effective/trusted decoding mode.
	decoder := yaml.NewDecoder(bytes.NewReader(got))
	var values []any
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	var decodedJSON []any
	if err := json.Unmarshal(jsonBytes, &decodedJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, decodedJSON) {
		t.Fatalf("YAML/JSON round-trip mismatch: %#v %#v", values, decodedJSON)
	}
}

func TestNativeNumbersStringsAndCancellation(t *testing.T) {
	large := "18446744073709551616000000001"
	value := api.MapValue(api.FieldValue{Name: "integer", Value: api.IntegerValue(large)}, api.FieldValue{Name: "fraction", Value: api.NumberValue("1.25")}, api.FieldValue{Name: "text", Value: api.StringValue("<literal>\a\n\\u003c\"é")}, api.FieldValue{Name: "bool", Value: api.BoolValue(false)})
	object := api.NewObject(api.CustomPlaybook, "reserved", api.Value{}, api.MapValue(api.FieldValue{Name: "extraVars", Value: value}))
	data, err := JSON(context.Background(), api.NewCatalog([]api.Object{object}))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !bytes.Contains(data, []byte(large)) || bytes.Contains(data, []byte(`\u003cliteral`)) {
		t.Fatalf("invalid JSON values: %s", data)
	}
	var decoded []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	vars := decoded[0]["spec"].(map[string]any)["extraVars"].(map[string]any)
	if vars["integer"].(json.Number).String() != large || vars["text"] != value.Get("text").Text() {
		t.Fatal("native values changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := YAML(ctx, api.NewCatalog([]api.Object{object})); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := JSON(ctx, api.NewCatalog([]api.Object{object})); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
