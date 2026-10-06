package ansible

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// decodeExact reads one JSON document keeping every number's text.
func decodeExact(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("the document does not decode: %v\n%s", err, data)
	}
	return decoded
}

// unmarked undoes ExtraVariables, failing on any string that is not the one
// member of an __ansible_unsafe object.
func unmarked(t *testing.T, value any, path string) any {
	t.Helper()
	switch typed := value.(type) {
	case string:
		t.Fatalf("%s is the plain string %q, which ansible-core would read as a template", path, typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = unmarked(t, item, path+"[]")
		}
		return out
	case map[string]any:
		if text, found := typed["__ansible_unsafe"]; found {
			if _, isString := text.(string); !isString || len(typed) != 1 {
				t.Fatalf("%s is not an object whose one member is __ansible_unsafe: %v", path, typed)
			}
			return text
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = unmarked(t, item, path+"."+key)
		}
		return out
	}
	return value
}

func TestExtraVariablesMarkEveryStringAsData(t *testing.T) {
	value := map[string]any{
		"{{ k }}": map[string]any{
			"empty":  "",
			"code":   "{{ 7*6 }}",
			"nested": []any{[]any{"{% raw %}kept{% endraw %}", json.Number("18446744073709551615")}, map[string]any{"{# x #}": "item"}},
		},
		"numbers": []any{json.Number("18446744073709551615"), json.Number("-1"), json.Number("0")},
		"flag":    true,
		"absent":  nil,
		"text":    "plain",
	}
	encoded, err := ExtraVariables(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"{{ k }}":`) || !strings.Contains(string(encoded), `"{# x #}":`) {
		t.Fatalf("a key did not stay a plain key: %s", encoded)
	}
	for _, text := range []string{"18446744073709551615,-1,0]", `"flag":true`, `"absent":null`} {
		if !strings.Contains(string(encoded), text) {
			t.Fatalf("%q did not stay as it was: %s", text, encoded)
		}
	}
	want := decodeExact(t, mustMarshal(t, value))
	if got := unmarked(t, decodeExact(t, encoded), "$"); !reflect.DeepEqual(got, want) {
		t.Fatalf("unmarking gave %v, want %v", got, want)
	}
}

// ansible-core decodes an object holding one of its type keys as that type and
// rereads a document it cannot decode as YAML, where every string is trusted,
// so a reserved key anywhere refuses rather than unmarking the whole document.
// Its decoder matches those three keys exactly, so any other key, one that
// merely starts as they do included, is an ordinary key a frozen request may
// already carry and still encodes.
func TestExtraVariablesRefuseAKeyAnsibleCoreReserves(t *testing.T) {
	for _, key := range []string{"__ansible_unsafe", "__ansible_vault", "__ansible_type"} {
		if encoded, err := ExtraVariables(map[string]any{"request": []any{map[string]any{key: "x"}}}); err == nil {
			t.Fatalf("the key %s was encoded: %s", key, encoded)
		}
	}
	for _, key := range []string{"_ansible_unsafe", "ansible_unsafe__", "__ansible", "__ansible_note", "__ansible_unsafe_", "__ANSIBLE_VAULT"} {
		encoded, err := ExtraVariables(map[string]any{"request": []any{map[string]any{key: "x"}}})
		if err != nil {
			t.Fatalf("the ordinary key %s refused: %v", key, err)
		}
		if want := `{"request":[{"` + key + `":{"__ansible_unsafe":"x"}}]}`; string(encoded) != want {
			t.Fatalf("the ordinary key %s encoded as %s, want %s", key, encoded, want)
		}
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
