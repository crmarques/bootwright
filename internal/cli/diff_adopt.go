package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/workspace"
)

// adoptSummary reports the outcome of `diff --adopt`: the history snapshot taken
// before writing, the edits applied, the new object files created, and the
// differences detected but deliberately not auto-adopted (which the operator
// edits by hand). Detected keeps adopt honest: it never silently drops a
// difference it cannot safely fold in.
type adoptSummary struct {
	Snapshot string   `json:"snapshot,omitempty"`
	Applied  []string `json:"applied,omitempty"`
	NewFiles []string `json:"newFiles,omitempty"`
	Detected []string `json:"detected,omitempty"`
}

func (s adoptSummary) empty() bool {
	return len(s.Applied) == 0 && len(s.NewFiles) == 0
}

// nodeEdit sets a scalar at a mapping-key path within the desired-state document
// whose metadata.name matches objectName, creating intermediate mappings.
type nodeEdit struct {
	objectName string
	path       []string
	value      string
	tag        string // "!!int" for numeric fields, "!!str" for string fields
}

// adoptLiveState folds the live state of probed managed storage clusters into
// desired YAML through the centralized ApplyInputEdits (which snapshots history
// first). Only the cleanly-representable facets are auto-adopted — replicated
// pool sizing, declared ceph config values, and pools that exist only on the
// cluster; everything else is reported as detected-but-not-adopted so the
// operator can edit it deliberately. It returns the summary and never mutates
// files outside the context input tree.
func adoptLiveState(cf *commonFlags, state v1alpha1.State, live liveDiffReport) (adoptSummary, error) {
	edits, summary, err := computeAdoptEdits(cf.ctx, state, live)
	if err != nil {
		return adoptSummary{}, err
	}
	if len(edits) == 0 {
		return summary, nil
	}
	snapshot, err := workspace.ApplyInputEdits(cf.ctx, "diff adopt", edits)
	if err != nil {
		return adoptSummary{}, err
	}
	summary.Snapshot = snapshot
	sort.Strings(summary.Applied)
	sort.Strings(summary.NewFiles)
	sort.Strings(summary.Detected)
	return summary, nil
}

func computeAdoptEdits(ctx workspace.Context, state v1alpha1.State, live liveDiffReport) ([]workspace.InputEdit, adoptSummary, error) {
	var summary adoptSummary
	clusterByName := map[string]v1alpha1.StorageCluster{}
	for _, cluster := range state.StorageClusters {
		clusterByName[cluster.Metadata.Name] = cluster
	}
	poolByCluster := map[string]map[string]v1alpha1.StoragePool{}
	for _, pool := range state.StoragePools {
		cluster := pool.Spec.StorageClusterRef.Name
		if poolByCluster[cluster] == nil {
			poolByCluster[cluster] = map[string]v1alpha1.StoragePool{}
		}
		poolByCluster[cluster][pool.Metadata.Name] = pool
	}

	// Mutations grouped by absolute source file, and new object files by absolute
	// target path.
	fileEdits := map[string][]nodeEdit{}
	newFiles := map[string][]byte{}

	for _, storage := range live.Storage {
		if !storage.Probed {
			continue
		}
		cluster := clusterByName[storage.Cluster]
		for _, facet := range storage.Report.Facets {
			switch facet.Name {
			case "pools":
				adoptPools(facet, storage.Cluster, cluster, poolByCluster[storage.Cluster], fileEdits, newFiles, &summary)
			case "config":
				adoptConfig(facet, cluster, fileEdits, &summary)
			default:
				for _, object := range facet.Objects {
					summary.Detected = append(summary.Detected, fmt.Sprintf("%s: %s %s (%s) — adjust the YAML manually", storage.Cluster, facet.Name, object.Key, object.State))
				}
			}
		}
	}

	edits := make([]workspace.InputEdit, 0, len(fileEdits)+len(newFiles))
	for path, mutations := range fileEdits {
		content, err := applyNodeEdits(path, mutations)
		if err != nil {
			return nil, adoptSummary{}, err
		}
		rel, err := inputRelPath(ctx.InputDir, path)
		if err != nil {
			return nil, adoptSummary{}, err
		}
		edits = append(edits, workspace.InputEdit{RelPath: rel, Content: content})
	}
	for path, content := range newFiles {
		rel, err := inputRelPath(ctx.InputDir, path)
		if err != nil {
			return nil, adoptSummary{}, err
		}
		edits = append(edits, workspace.InputEdit{RelPath: rel, Content: content})
	}
	// Deterministic order for stable history and output.
	sort.Slice(edits, func(i, j int) bool { return edits[i].RelPath < edits[j].RelPath })
	return edits, summary, nil
}

// adoptPools folds replicated pool sizing changes into the declaring pool's file
// and synthesizes a new StoragePool file for a pool that exists only on the
// cluster. Structural changes (type, erasure profile) and other tunables are
// detected, not auto-adopted.
func adoptPools(facet cephdiff.FacetDiff, clusterName string, cluster v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool, fileEdits map[string][]nodeEdit, newFiles map[string][]byte, summary *adoptSummary) {
	for _, object := range facet.Objects {
		switch object.State {
		case cephdiff.ObjectChanged:
			pool, ok := pools[object.Key]
			if !ok || pool.SourcePath == "" {
				summary.Detected = append(summary.Detected, fmt.Sprintf("%s: pool %s changed but its source file is unknown", clusterName, object.Key))
				continue
			}
			for _, field := range object.Fields {
				switch field.Name {
				case "size":
					fileEdits[pool.SourcePath] = append(fileEdits[pool.SourcePath], nodeEdit{objectName: pool.Metadata.Name, path: []string{"spec", "ceph", "replicated", "size"}, value: field.Real, tag: "!!int"})
					summary.Applied = append(summary.Applied, fmt.Sprintf("%s: pool %s size %s→%s", clusterName, object.Key, field.Desired, field.Real))
				case "min_size":
					fileEdits[pool.SourcePath] = append(fileEdits[pool.SourcePath], nodeEdit{objectName: pool.Metadata.Name, path: []string{"spec", "ceph", "replicated", "minSize"}, value: field.Real, tag: "!!int"})
					summary.Applied = append(summary.Applied, fmt.Sprintf("%s: pool %s min_size %s→%s", clusterName, object.Key, field.Desired, field.Real))
				default:
					summary.Detected = append(summary.Detected, fmt.Sprintf("%s: pool %s %s %s→%s — adjust the YAML manually", clusterName, object.Key, field.Name, field.Desired, field.Real))
				}
			}
		case cephdiff.ObjectRealOnly:
			path, content, err := synthesizePoolFile(cluster, object)
			if err != nil {
				summary.Detected = append(summary.Detected, fmt.Sprintf("%s: pool %s only on cluster — could not synthesize (%v)", clusterName, object.Key, err))
				continue
			}
			newFiles[path] = content
			summary.NewFiles = append(summary.NewFiles, filepath.Base(path))
		default:
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: pool %s declared but absent on the cluster (left in desired)", clusterName, object.Key))
		}
	}
}

func adoptConfig(facet cephdiff.FacetDiff, cluster v1alpha1.StorageCluster, fileEdits map[string][]nodeEdit, summary *adoptSummary) {
	if cluster.SourcePath == "" {
		return
	}
	for _, object := range facet.Objects {
		section, key, ok := strings.Cut(object.Key, "/")
		if !ok {
			continue
		}
		// public_network / cluster_network are owned by spec.ceph.networks, not
		// config; adopting them under config would be rejected on the next load.
		if section == "global" && (key == "public_network" || key == "cluster_network") {
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: %s is owned by spec.ceph.networks — adjust there", cluster.Metadata.Name, key))
			continue
		}
		if object.State == cephdiff.ObjectChanged {
			var real string
			for _, field := range object.Fields {
				if field.Name == "value" {
					real = field.Real
				}
			}
			fileEdits[cluster.SourcePath] = append(fileEdits[cluster.SourcePath], nodeEdit{objectName: cluster.Metadata.Name, path: []string{"spec", "ceph", "config", section, key}, value: real, tag: "!!str"})
			summary.Applied = append(summary.Applied, fmt.Sprintf("%s: config %s = %s", cluster.Metadata.Name, object.Key, real))
		} else {
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: config %s (%s) — adjust the YAML manually", cluster.Metadata.Name, object.Key, object.State))
		}
	}
}

// synthesizePoolFile builds a minimal StoragePool object for a pool discovered on
// the cluster but not declared, and returns its target path (a sibling of the
// cluster's source file) and marshaled YAML.
func synthesizePoolFile(cluster v1alpha1.StorageCluster, object cephdiff.ObjectDiff) (string, []byte, error) {
	if cluster.SourcePath == "" {
		return "", nil, fmt.Errorf("cluster source file unknown")
	}
	pool := v1alpha1.StoragePool{
		APIVersion: v1alpha1.APIVersion,
		Kind:       v1alpha1.KindStoragePool,
		Metadata:   v1alpha1.Metadata{Name: object.Key},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: cluster.Metadata.Name},
			Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated},
		},
	}
	for _, field := range object.Fields {
		switch field.Name {
		case "type":
			pool.Spec.Ceph.Type = field.Real
		case "size":
			pool.Spec.Ceph.Replicated.Size = atoiOrZero(field.Real)
		case "min_size":
			pool.Spec.Ceph.Replicated.MinSize = atoiOrZero(field.Real)
		case "application":
			pool.Spec.Ceph.Application = field.Real
		}
	}
	data, err := yaml.Marshal(pool)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(filepath.Dir(cluster.SourcePath), object.Key+".yaml")
	return path, data, nil
}

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

func inputRelPath(inputDir, absPath string) (string, error) {
	rel, err := filepath.Rel(inputDir, absPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s under %s: %w", absPath, inputDir, err)
	}
	return filepath.ToSlash(rel), nil
}

func atoiOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
