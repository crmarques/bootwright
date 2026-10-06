package encoding

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

func quotingCorpus() []string {
	return []string{"ends:", "k:", "...x", "x\u2028y", "x\u2029y", "x\u0085y", "x\ufeffy", "a\x7fb", "a: b", "a #b", "plain-value", "9.8", "True"}
}

func TestPlainStringsAreWrittenPlainAndOthersDoubleQuoted(t *testing.T) {
	fields := []api.FieldValue{}
	for _, text := range quotingCorpus() {
		fields = append(fields, api.FieldValue{Name: text, Value: api.StringValue(text)})
		if !plainString(text) {
			continue
		}
		encoded, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text})
		if err != nil || string(encoded) != text+"\n" {
			t.Errorf("plain %q encoded as %q, %v", text, encoded, err)
		}
	}
	playbook := api.NewObject(api.CustomPlaybook, "probe", api.Value{}, api.MapValue(api.FieldValue{Name: "extraVars", Value: api.MapValue(fields...)}))
	out, err := YAML(context.Background(), api.NewCatalog([]api.Object{playbook}))
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(out, &document); err != nil {
		t.Fatalf("effective YAML does not decode: %v\n%s", err, out)
	}
	spec := childNode(t, document.Content[0], "spec")
	extraVars := childNode(t, spec, "extraVars")
	decoded := map[string]string{}
	for i := 0; i+1 < len(extraVars.Content); i += 2 {
		for _, node := range extraVars.Content[i : i+2] {
			if want := plainString(node.Value); node.Style != 0 && node.Style != yaml.DoubleQuotedStyle || want != (node.Style == 0) {
				t.Errorf("%q was written with style %v; plain is %v", node.Value, node.Style, want)
			}
		}
		decoded[extraVars.Content[i].Value] = extraVars.Content[i+1].Value
	}
	for _, text := range quotingCorpus() {
		if decoded[text] != text {
			t.Errorf("%q decoded back as %q", text, decoded[text])
		}
	}
	if len(decoded) != len(quotingCorpus()) {
		t.Errorf("decoded %d entries, want %d:\n%s", len(decoded), len(quotingCorpus()), out)
	}
}

func childNode(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	t.Fatalf("no %s in the effective YAML", key)
	return nil
}
