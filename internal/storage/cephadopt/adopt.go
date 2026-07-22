package cephadopt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"github.com/crmarques/bootwright/internal/workspace"
)

type ProbedStorage struct {
	Cluster string
	Report  cephdiff.Report
}

type Summary struct {
	Snapshot string   `json:"snapshot,omitempty"`
	Applied  []string `json:"applied,omitempty"`
	NewFiles []string `json:"newFiles,omitempty"`
	Detected []string `json:"detected,omitempty"`
}

func (s Summary) Empty() bool {
	return len(s.Applied) == 0 && len(s.NewFiles) == 0
}

type nodeEdit struct {
	objectName string
	path       []string
	value      string
	tag        string

	hostRef      string
	hostPath     []string
	appendValues []string
}

func Adopt(ctx workspace.Context, state v1alpha1.State, probed []ProbedStorage) (Summary, error) {
	edits, summary, err := ComputeEdits(ctx, state, probed)
	if err != nil {
		return Summary{}, err
	}
	if len(edits) == 0 {
		return summary, nil
	}
	snapshot, err := workspace.ApplyInputEdits(ctx, "diff adopt", edits)
	if err != nil {
		return Summary{}, err
	}
	summary.Snapshot = snapshot
	sort.Strings(summary.Applied)
	sort.Strings(summary.NewFiles)
	sort.Strings(summary.Detected)
	return summary, nil
}

func ComputeEdits(ctx workspace.Context, state v1alpha1.State, probed []ProbedStorage) ([]workspace.InputEdit, Summary, error) {
	var summary Summary
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

	fileEdits := map[string][]nodeEdit{}
	newFiles := map[string][]byte{}

	for _, storage := range probed {
		cluster := clusterByName[storage.Cluster]
		for _, facet := range storage.Report.Facets {
			switch facet.Name {
			case "pools":
				adoptPools(facet, storage.Cluster, cluster, poolByCluster[storage.Cluster], fileEdits, newFiles, &summary)
			case "config":
				adoptConfig(facet, cluster, fileEdits, &summary)
			case "osd-devices":
				adoptOSDDevices(facet, storage.Cluster, cluster, fileEdits, &summary)
			default:
				for _, object := range facet.Objects {
					summary.Detected = append(summary.Detected, fmt.Sprintf("%s: %s %s (%s) — adjust the YAML manually", storage.Cluster, facet.Name, object.Key, object.State))
				}
			}
		}
		for _, adv := range storage.Report.UnpinnedOSDHosts {
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s consumes [%s] via a filter/all selection — pin osd.dataDevices.paths for exact reconstruction", storage.Cluster, adv.Host, strings.Join(adv.Devices, " ")))
		}
	}

	edits := make([]workspace.InputEdit, 0, len(fileEdits)+len(newFiles))
	for path, mutations := range fileEdits {
		content, err := applyNodeEdits(path, mutations)
		if err != nil {
			return nil, Summary{}, err
		}
		rel, err := inputRelPath(ctx.InputDir, path)
		if err != nil {
			return nil, Summary{}, err
		}
		edits = append(edits, workspace.InputEdit{RelPath: rel, Content: content})
	}
	for path, content := range newFiles {
		rel, err := inputRelPath(ctx.InputDir, path)
		if err != nil {
			return nil, Summary{}, err
		}
		if _, statErr := os.Stat(path); statErr == nil {
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s already exists — a live-only object would overwrite it; author it manually", rel))
			continue
		}
		edits = append(edits, workspace.InputEdit{RelPath: rel, Content: content})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].RelPath < edits[j].RelPath })
	return edits, summary, nil
}

func adoptPools(facet cephdiff.FacetDiff, clusterName string, cluster v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool, fileEdits map[string][]nodeEdit, newFiles map[string][]byte, summary *Summary) {
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

func adoptConfig(facet cephdiff.FacetDiff, cluster v1alpha1.StorageCluster, fileEdits map[string][]nodeEdit, summary *Summary) {
	if cluster.SourcePath == "" {
		return
	}
	for _, object := range facet.Objects {
		section, key, ok := strings.Cut(object.Key, "/")
		if !ok {
			continue
		}
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

func adoptOSDDevices(facet cephdiff.FacetDiff, clusterName string, cluster v1alpha1.StorageCluster, fileEdits map[string][]nodeEdit, summary *Summary) {
	for _, object := range facet.Objects {
		field, ok := osdDevicesField(object)
		switch object.State {
		case cephdiff.ObjectChanged:
			if !ok || !field.HasDesired || !field.HasReal {
				summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s devices differ — adjust the YAML manually", clusterName, object.Key))
				continue
			}
			added := subtractFields(field.Real, field.Desired)
			removed := subtractFields(field.Desired, field.Real)
			host, hostOK := topology.CephNodeByName(cluster, object.Key)
			seqPath, canAppend := hostDeviceSequencePath(host)
			switch {
			case len(added) > 0 && hostOK && canAppend && cluster.SourcePath != "":
				fileEdits[cluster.SourcePath] = append(fileEdits[cluster.SourcePath], nodeEdit{
					objectName:   cluster.Metadata.Name,
					hostRef:      host.MachineRef.Name,
					hostPath:     seqPath,
					appendValues: devicePaths(added),
				})
				summary.Applied = append(summary.Applied, fmt.Sprintf("%s: osd host %s pin +[%s]", clusterName, object.Key, strings.Join(added, " ")))
			case len(added) > 0:
				summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s grew OSDs on [%s] but its devices are not a plain per-host path list — pin manually", clusterName, object.Key, strings.Join(added, " ")))
			}
			if len(removed) > 0 {
				summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s declares [%s] that no longer backs an OSD — investigate; left in desired", clusterName, object.Key, strings.Join(removed, " ")))
			}
		case cephdiff.ObjectDesiredOnly:
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s declares devices but has no OSDs on the cluster — left in desired", clusterName, object.Key))
		default:
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s (%s) — adjust the YAML manually", clusterName, object.Key, object.State))
		}
	}
}

func osdDevicesField(object cephdiff.ObjectDiff) (cephdiff.FieldDiff, bool) {
	for _, field := range object.Fields {
		if field.Name == "devices" {
			return field, true
		}
	}
	return cephdiff.FieldDiff{}, false
}

func hostDeviceSequencePath(host v1alpha1.StorageCephNode) (path []string, canAppend bool) {
	if len(host.Devices) > 0 {
		return []string{"devices"}, true
	}
	if host.OSD != nil && host.OSD.DataDevices != nil && len(host.OSD.DataDevices.Paths) > 0 {
		return []string{"osd", "dataDevices", "paths"}, true
	}
	return nil, false
}

func subtractFields(a, b string) []string {
	inB := map[string]bool{}
	for _, tok := range strings.Fields(b) {
		inB[tok] = true
	}
	var out []string
	for _, tok := range strings.Fields(a) {
		if !inB[tok] {
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

func devicePaths(basenames []string) []string {
	out := make([]string, len(basenames))
	for i, name := range basenames {
		out[i] = "/dev/" + name
	}
	return out
}

func synthesizePoolFile(cluster v1alpha1.StorageCluster, object cephdiff.ObjectDiff) (string, []byte, error) {
	if cluster.SourcePath == "" {
		return "", nil, fmt.Errorf("cluster source file unknown")
	}
	for _, field := range object.Fields {
		if field.Name == "type" && field.Real == v1alpha1.StoragePoolTypeErasureCode {
			return "", nil, fmt.Errorf("erasure-coded pool needs a hand-authored spec.ceph.erasure profile (dataChunks/codingChunks); adopt cannot reconstruct it")
		}
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
