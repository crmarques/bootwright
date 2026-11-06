package clusterobjects

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

func TestNativeProjectionStringsAreJSON(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "plugins", "module_utils", "cluster_objects_native.py"))
	if err != nil {
		t.Fatal(err)
	}
	block := strings.Split(strings.Split(string(source), "JSON_TEMPLATE = (")[1], "\n\n")[0]
	var definition strings.Builder
	for _, literal := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(block, -1) {
		definition.WriteString(literal[1])
	}
	tmpl, err := template.New("projection").Parse(definition.String() + `{{template "json" .}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"\a", "\v", "\x1b", "\U000e0001", "\u2028", "\x00\t\r\n\\\"<>&'", "é"} {
		t.Run(strconv.QuoteToASCII(value), func(t *testing.T) {
			wanted := map[string]any{value: []any{value}}
			var output bytes.Buffer
			if err := tmpl.Execute(&output, wanted); err != nil {
				t.Fatal(err)
			}
			var wire []any
			if err := json.Unmarshal(output.Bytes(), &wire); err != nil {
				t.Fatalf("projection is not JSON: %q: %v", output.String(), err)
			}
			got := decodeProjectedTestValue(t, wire)
			if !reflect.DeepEqual(got, wanted) {
				t.Fatalf("projection changed value: %#v", got)
			}
		})
	}
}

func TestPlanningRefusesUnsupportedSecretPlacementType(t *testing.T) {
	for _, kind := range []string{"", "kubernetes.io/dockerconfigjson", "kubernetes.io/service-account-token", "other"} {
		t.Run(kind, func(t *testing.T) {
			steps, err := CompilePhase(Phase{Secrets: []SecretPlacement{{Secret: "input", Namespace: "operators", Name: "credentials", Type: kind, Keys: []KeyPart{{Key: "password", Part: "password"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			data, err := Freeze(Program{Version: Version, Steps: steps, Checks: []Check{}}, Options{PlacementNamespaces: []string{"operators"}})
			if err == nil || len(data) != 0 {
				t.Fatalf("unsupported type froze a request: %s, %v", data, err)
			}
		})
	}
}

func decodeProjectedTestValue(t *testing.T, wire []any) any {
	t.Helper()
	switch wire[0] {
	case "map":
		result := map[string]any{}
		for key, value := range wire[1].(map[string]any) {
			decoded, err := hex.DecodeString(key)
			if err != nil {
				t.Fatal(err)
			}
			result[string(decoded)] = decodeProjectedTestValue(t, value.([]any))
		}
		return result
	case "string":
		decoded, err := hex.DecodeString(wire[1].(string))
		if err != nil {
			t.Fatal(err)
		}
		return string(decoded)
	case "list":
		result := []any{}
		for _, value := range wire[1].([]any) {
			result = append(result, decodeProjectedTestValue(t, value.([]any)))
		}
		return result
	default:
		return wire[1]
	}
}
