package contexts

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"go.yaml.in/yaml/v3"
)

const MaxConfigurationBytes = 64 << 10

const configurationFileRemediation = "pass desired state with --input-dir <dir>; -f takes one standalone Context file"

func ConfigurationError(message string) error {
	return diagnostics.NewFailure("context.configuration", message, "")
}

func DefaultConfiguration(name string) Configuration {
	return Configuration{Name: name, SecretStore: SecretStoreConfiguration{Type: "local-keyring"}}
}

func (c Configuration) Canonical() []byte {
	return fmt.Appendf(nil, "apiVersion: bootwright.io/v1alpha1\nkind: Context\nmetadata:\n  name: %q\n\nspec:\n  secretStore:\n    type: %q\n", c.Name, c.SecretStore.Type)
}

// ParseConfiguration admits only a standalone controller configuration document.
func ParseConfiguration(name string, data []byte) (Configuration, error) {
	if validName(name) != nil || len(data) == 0 || len(data) > MaxConfigurationBytes || !utf8.Valid(data) {
		return Configuration{}, ConfigurationError("Context configuration must be bounded UTF-8 YAML with a valid context name")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document, extra yaml.Node
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&extra) != io.EOF || len(document.Content) != 1 {
		return Configuration{}, ConfigurationError("Context configuration requires exactly one valid YAML document")
	}
	budget := 2048
	if !safeConfigurationNode(&document, 0, &budget) {
		return Configuration{}, ConfigurationError("Context configuration contains unsupported YAML syntax or exceeds structural limits")
	}
	if root := document.Content[0]; mapping(root) {
		apiVersion, kind := mappingValue(root, "apiVersion"), mappingValue(root, "kind")
		if configurationString(apiVersion) != "bootwright.io/v1alpha1" || configurationString(kind) != "Context" {
			return Configuration{}, diagnostics.NewFailureWithRemediation("context.configuration",
				"the file declares "+declared("apiVersion", apiVersion)+" and "+declared("kind", kind)+
					"; a Context file declares apiVersion bootwright.io/v1alpha1 and kind Context", "",
				configurationFileRemediation)
		}
	}
	envelope, err := configurationMapping(document.Content[0], "", "apiVersion", "kind", "metadata", "spec")
	if err != nil {
		return Configuration{}, err
	}
	metadata, err := configurationMapping(envelope["metadata"], "metadata", "name")
	if err != nil && mapping(envelope["metadata"]) {
		return Configuration{}, err
	}
	if err != nil || configurationString(metadata["name"]) != name {
		return Configuration{}, ConfigurationError("Context metadata.name must match --name")
	}
	result := DefaultConfiguration(name)
	if envelope["spec"] == nil {
		return result, nil
	}
	spec, err := configurationMapping(envelope["spec"], "spec", "secretStore")
	if err != nil {
		return Configuration{}, err
	}
	if spec["secretStore"] == nil {
		return result, nil
	}
	store, err := configurationMapping(spec["secretStore"], "spec.secretStore", "type")
	if err != nil {
		return Configuration{}, err
	}
	if store["type"] != nil {
		result.SecretStore.Type = configurationString(store["type"])
		if !namePattern.MatchString(result.SecretStore.Type) {
			return Configuration{}, ConfigurationError("secretStore.type must be a safe implementation identifier")
		}
	}
	return result, nil
}

func mapping(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.MappingNode && node.Tag == "!!map" && len(node.Content)%2 == 0
}

// mappingValue answers a mapping's first value under key, so what a document
// declares itself to be is named before any field it carries is judged.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for index := 0; index < len(node.Content); index += 2 {
		if configurationString(node.Content[index]) == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// configurationMapping admits one mapping of known fields; parent is the
// mapping's own field path, which a refusal names with the offending key and
// its line.
func configurationMapping(node *yaml.Node, parent string, fields ...string) (map[string]*yaml.Node, error) {
	if !mapping(node) {
		return nil, ConfigurationError("Context configuration requires non-null mappings")
	}
	result := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		key := configurationString(keyNode)
		switch {
		case !slices.Contains(fields, key):
			return nil, configurationFieldError(parent, keyNode, "is unknown")
		case result[key] != nil:
			return nil, configurationFieldError(parent, keyNode, "is duplicated")
		}
		result[key] = node.Content[index+1]
	}
	return result, nil
}

func configurationFieldError(parent string, key *yaml.Node, reason string) error {
	field := declaredValue(key)
	switch {
	case field == "":
		field = "with a non-string or over-long key"
	case parent != "":
		field = parent + "." + field
	}
	return ConfigurationError("Context configuration field " + field + " at line " + strconv.Itoa(key.Line) + " " + reason)
}

// declared names what the document says for one envelope field, bounded so a
// refusal never repeats an arbitrary document back.
func declared(field string, node *yaml.Node) string {
	switch {
	case node == nil:
		return "no " + field
	case node.Kind != yaml.ScalarNode || node.Tag != "!!str":
		return "a non-string " + field
	case node.Value == "":
		return "an empty " + field
	case declaredValue(node) == "":
		return "an over-long " + field
	}
	return field + " " + node.Value
}

func declaredValue(node *yaml.Node) string {
	value := configurationString(node)
	if len(value) > 64 {
		return ""
	}
	return value
}

func configurationString(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return ""
	}
	return node.Value
}

func safeConfigurationNode(node *yaml.Node, depth int, budget *int) bool {
	*budget--
	if *budget < 0 || depth > 16 || node.Anchor != "" || node.Alias != nil || node.Kind == yaml.AliasNode || node.Style&yaml.TaggedStyle != 0 {
		return false
	}
	for _, child := range node.Content {
		if !safeConfigurationNode(child, depth+1, budget) {
			return false
		}
	}
	return true
}
