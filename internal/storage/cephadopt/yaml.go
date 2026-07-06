package cephadopt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"go.yaml.in/yaml/v3"
)

// This file holds the low-level YAML-document mutation primitives `diff --adopt`
// uses: load/marshal a multi-document desired-state file preserving comments, and
// apply the two kinds of edit (scalar set, sequence append) a Summary's nodeEdits
// describe. The adopt policy — which differences fold in and which are only
// reported — lives in adopt.go.

// applyNodeEdits loads a desired-state file's documents, applies each edit to the
// matching document, and re-marshals the whole file, preserving comments and the
// order of unaffected documents.
func applyNodeEdits(path string, edits []nodeEdit) ([]byte, error) {
	docs, err := loadYAMLDocuments(path)
	if err != nil {
		return nil, err
	}
	for _, edit := range edits {
		doc := findDocumentByName(docs, edit.objectName)
		if doc == nil {
			return nil, fmt.Errorf("adopt %s: no document named %q", path, edit.objectName)
		}
		if edit.appendValues != nil {
			if err := appendHostSequence(doc, edit.hostRef, edit.hostPath, edit.appendValues); err != nil {
				return nil, fmt.Errorf("adopt %s: %w", path, err)
			}
			continue
		}
		if err := setScalarAtPath(doc, edit.path, edit.value, edit.tag); err != nil {
			return nil, fmt.Errorf("adopt %s: %w", path, err)
		}
	}
	return marshalYAMLDocuments(docs)
}

func loadYAMLDocuments(path string) ([]*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var docs []*yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		docNode := node
		docs = append(docs, &docNode)
	}
	return docs, nil
}

func marshalYAMLDocuments(docs []*yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	for _, doc := range docs {
		if err := encoder.Encode(doc); err != nil {
			_ = encoder.Close()
			return nil, err
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// appendHostSequence appends each value (as a !!str scalar, skipping ones already
// present) to the sequence at hostPath within the spec.ceph.topology.hosts[]
// element identified by hostRef (its machineRef.name or hostname). It is purely
// additive: it never rewrites or removes an existing entry, and it creates the
// target sequence only if the host declares none yet.
func appendHostSequence(doc *yaml.Node, hostRef string, hostPath, values []string) error {
	mapping := doc
	if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
		mapping = mapping.Content[0]
	}
	hosts := mappingSequenceAtPath(mapping, []string{"spec", "ceph", "topology", "hosts"})
	if hosts == nil {
		return fmt.Errorf("no spec.ceph.topology.hosts sequence")
	}
	host := findHostElement(hosts, hostRef)
	if host == nil {
		return fmt.Errorf("no hosts[] element matching %q", hostRef)
	}
	seq, err := ensureSequenceAtPath(host, hostPath)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, item := range seq.Content {
		present[item.Value] = true
	}
	for _, value := range values {
		if present[value] {
			continue
		}
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		present[value] = true
	}
	return nil
}

// mappingSequenceAtPath follows a mapping-key path to a sequence node, or nil if
// any element is missing or not the expected kind.
func mappingSequenceAtPath(mapping *yaml.Node, path []string) *yaml.Node {
	cur := mapping
	for _, key := range path {
		if cur.Kind != yaml.MappingNode {
			return nil
		}
		cur = mappingValue(cur, key)
		if cur == nil {
			return nil
		}
	}
	if cur.Kind != yaml.SequenceNode {
		return nil
	}
	return cur
}

// findHostElement returns the hosts[] mapping whose hostname or machineRef.name
// equals ref.
func findHostElement(hosts *yaml.Node, ref string) *yaml.Node {
	for _, host := range hosts.Content {
		if host.Kind != yaml.MappingNode {
			continue
		}
		if hostname := mappingValue(host, "hostname"); hostname != nil && hostname.Value == ref {
			return host
		}
		if machineRef := mappingValue(host, "machineRef"); machineRef != nil {
			if name := mappingValue(machineRef, "name"); name != nil && name.Value == ref {
				return host
			}
		}
	}
	return nil
}

// ensureSequenceAtPath resolves the sequence at a mapping-key path within mapping,
// creating any missing intermediate mappings and an empty final sequence.
func ensureSequenceAtPath(mapping *yaml.Node, path []string) (*yaml.Node, error) {
	cur := mapping
	for i, key := range path {
		last := i == len(path)-1
		child := mappingValue(cur, key)
		if last {
			if child == nil {
				child = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
			}
			if child.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("path element %q is not a sequence", key)
			}
			return child, nil
		}
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
		}
		if child.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("path element %q is not a mapping", key)
		}
		cur = child
	}
	return nil, fmt.Errorf("empty path")
}

// findDocumentByName returns the mapping node of the document whose metadata.name
// equals name.
func findDocumentByName(docs []*yaml.Node, name string) *yaml.Node {
	for _, doc := range docs {
		mapping := doc
		if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
			mapping = mapping.Content[0]
		}
		if mapping.Kind != yaml.MappingNode {
			continue
		}
		metadata := mappingValue(mapping, "metadata")
		if metadata == nil {
			continue
		}
		if nameNode := mappingValue(metadata, "name"); nameNode != nil && nameNode.Value == name {
			return mapping
		}
	}
	return nil
}

// setScalarAtPath sets value at the mapping-key path within mapping, creating any
// missing intermediate mappings.
func setScalarAtPath(doc *yaml.Node, path []string, value, tag string) error {
	mapping := doc
	if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
		mapping = mapping.Content[0]
	}
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("document root is not a mapping")
	}
	for i, key := range path {
		last := i == len(path)-1
		child := mappingValue(mapping, key)
		if last {
			scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
			if child != nil {
				child.Kind = yaml.ScalarNode
				child.Tag = tag
				child.Value = value
				child.Content = nil
				return nil
			}
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, scalar)
			return nil
		}
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
		}
		if child.Kind != yaml.MappingNode {
			return fmt.Errorf("path element %q is not a mapping", key)
		}
		mapping = child
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
