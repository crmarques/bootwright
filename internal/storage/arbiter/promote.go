package arbiter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"github.com/crmarques/bootwright/internal/workspace"
)

type Promotion struct {
	Cluster  string
	FromNode string
	ToNode   string
	Machine  string
	Site     string
	RelPath  string
	Content  []byte
}

func (p Promotion) Empty() bool { return len(p.Content) == 0 }

func ComputePromotion(ctx workspace.Context, cluster v1alpha1.StorageCluster, machineName string) (Promotion, error) {
	current, err := DesiredArbiter(cluster)
	if err != nil {
		return Promotion{}, err
	}
	if current.MachineRef.Name == machineName {
		return Promotion{Cluster: cluster.Metadata.Name, FromNode: current.Name, ToNode: current.Name, Machine: machineName, Site: current.Site}, nil
	}
	nodeName := machineName
	if existing, ok := topology.HostByName(cluster, nodeName); ok && existing.Name != current.Name {
		return Promotion{}, fmt.Errorf("StorageCluster/%s already declares a topology node named %q, so promoting Machine/%s would collide with it; author the replacement arbiter node yourself and re-run without --new-arbiter-machine", cluster.Metadata.Name, existing.Name, machineName)
	}
	if cluster.SourcePath == "" {
		return Promotion{}, fmt.Errorf("StorageCluster/%s was not loaded from a context input file, so its tiebreaker cannot be rewritten", cluster.Metadata.Name)
	}
	rel, err := filepath.Rel(ctx.InputDir, cluster.SourcePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return Promotion{}, fmt.Errorf("StorageCluster/%s is declared outside the context input directory (%s), so its tiebreaker cannot be rewritten", cluster.Metadata.Name, cluster.SourcePath)
	}
	content, err := rewriteTiebreaker(cluster.SourcePath, cluster.Metadata.Name, current.Name, nodeName, machineName)
	if err != nil {
		return Promotion{}, err
	}
	return Promotion{
		Cluster:  cluster.Metadata.Name,
		FromNode: current.Name,
		ToNode:   nodeName,
		Machine:  machineName,
		Site:     current.Site,
		RelPath:  filepath.ToSlash(rel),
		Content:  content,
	}, nil
}

func Apply(ctx workspace.Context, promotion Promotion) (string, error) {
	if promotion.Empty() {
		return "", nil
	}
	return workspace.ApplyInputEdits(ctx, "replace arbiter", []workspace.InputEdit{{RelPath: promotion.RelPath, Content: promotion.Content}})
}

func rewriteTiebreaker(path, clusterName, fromNode, toNode, machineName string) ([]byte, error) {
	docs, err := loadDocuments(path)
	if err != nil {
		return nil, err
	}
	doc := documentByName(docs, clusterName)
	if doc == nil {
		return nil, fmt.Errorf("no StorageCluster document named %q in %s", clusterName, path)
	}
	nodes := sequenceAtPath(doc, []string{"spec", "ceph", "topology", "nodes"})
	if nodes == nil {
		return nil, fmt.Errorf("no spec.ceph.topology.nodes sequence for StorageCluster/%s in %s", clusterName, path)
	}
	node := nodeElementNamed(nodes, fromNode)
	if node == nil {
		return nil, fmt.Errorf("no spec.ceph.topology.nodes[] entry named %q for StorageCluster/%s in %s", fromNode, clusterName, path)
	}
	setMappingScalar(node, "name", toNode)
	removeMappingKey(node, "fqdn")
	machineRef := mappingChild(node, "machineRef")
	if machineRef == nil || machineRef.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("spec.ceph.topology.nodes[] entry %q for StorageCluster/%s has no machineRef mapping", fromNode, clusterName)
	}
	setMappingScalar(machineRef, "name", machineName)
	stretch := mappingAtPath(doc, []string{"spec", "ceph", "topology", "stretch"})
	if stretch == nil {
		return nil, fmt.Errorf("no spec.ceph.topology.stretch mapping for StorageCluster/%s in %s", clusterName, path)
	}
	tiebreaker := mappingChild(stretch, "tiebreaker")
	if tiebreaker == nil || tiebreaker.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("no spec.ceph.topology.stretch.tiebreaker mapping for StorageCluster/%s in %s", clusterName, path)
	}
	setMappingScalar(tiebreaker, "node", toNode)
	return marshalDocuments(docs)
}

func loadDocuments(path string) ([]*yaml.Node, error) {
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

func marshalDocuments(docs []*yaml.Node) ([]byte, error) {
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

func documentByName(docs []*yaml.Node, name string) *yaml.Node {
	for _, doc := range docs {
		mapping := doc
		if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
			mapping = mapping.Content[0]
		}
		if mapping.Kind != yaml.MappingNode {
			continue
		}
		metadata := mappingChild(mapping, "metadata")
		if metadata == nil {
			continue
		}
		if nameNode := mappingChild(metadata, "name"); nameNode != nil && nameNode.Value == name {
			return mapping
		}
	}
	return nil
}

func mappingAtPath(mapping *yaml.Node, path []string) *yaml.Node {
	cur := mapping
	for _, key := range path {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return nil
		}
		cur = mappingChild(cur, key)
	}
	if cur == nil || cur.Kind != yaml.MappingNode {
		return nil
	}
	return cur
}

func sequenceAtPath(mapping *yaml.Node, path []string) *yaml.Node {
	cur := mapping
	for _, key := range path {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return nil
		}
		cur = mappingChild(cur, key)
	}
	if cur == nil || cur.Kind != yaml.SequenceNode {
		return nil
	}
	return cur
}

func nodeElementNamed(nodes *yaml.Node, name string) *yaml.Node {
	for _, node := range nodes.Content {
		if node.Kind != yaml.MappingNode {
			continue
		}
		if value := mappingChild(node, "name"); value != nil && value.Value == name {
			return node
		}
	}
	return nil
}

func mappingChild(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setMappingScalar(mapping *yaml.Node, key, value string) {
	if child := mappingChild(mapping, key); child != nil {
		child.Kind = yaml.ScalarNode
		child.Tag = "!!str"
		child.Value = value
		child.Content = nil
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func removeMappingKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
