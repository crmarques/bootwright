package contexts

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"go.yaml.in/yaml/v3"
)

const MaxConfigurationBytes = 64 << 10

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
	envelope, err := configurationMapping(document.Content[0], "apiVersion", "kind", "metadata", "spec")
	if err != nil {
		return Configuration{}, err
	}
	if configurationString(envelope["apiVersion"]) != "bootwright.io/v1alpha1" || configurationString(envelope["kind"]) != "Context" {
		return Configuration{}, ConfigurationError("configuration requires apiVersion bootwright.io/v1alpha1 and kind Context")
	}
	metadata, err := configurationMapping(envelope["metadata"], "name")
	if err != nil || configurationString(metadata["name"]) != name {
		return Configuration{}, ConfigurationError("Context metadata.name must match --name")
	}
	result := DefaultConfiguration(name)
	if envelope["spec"] == nil {
		return result, nil
	}
	spec, err := configurationMapping(envelope["spec"], "secretStore")
	if err != nil {
		return Configuration{}, err
	}
	if spec["secretStore"] == nil {
		return result, nil
	}
	store, err := configurationMapping(spec["secretStore"], "type")
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

func configurationMapping(node *yaml.Node, fields ...string) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode || node.Tag != "!!map" || len(node.Content)%2 != 0 {
		return nil, ConfigurationError("Context configuration requires non-null mappings")
	}
	result := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		key := configurationString(node.Content[index])
		allowed := false
		for _, field := range fields {
			allowed = allowed || key == field
		}
		if !allowed || result[key] != nil {
			return nil, ConfigurationError("Context configuration contains an unknown or duplicate field")
		}
		result[key] = node.Content[index+1]
	}
	return result, nil
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
