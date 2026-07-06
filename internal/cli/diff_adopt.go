package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/storage/topology"
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

// nodeEdit mutates the desired-state document whose metadata.name matches
// objectName. In its scalar form it sets value at the mapping-key path, creating
// intermediate mappings. In its sequence-append form (appendValues set) it
// appends each value (as a !!str scalar, skipping ones already present) to the
// sequence at hostPath within the spec.ceph.topology.hosts[] element identified
// by hostRef — the purely additive OSD-device pin, which never rewrites or
// removes an existing entry.
type nodeEdit struct {
	objectName string
	path       []string
	value      string
	tag        string // "!!int" for numeric fields, "!!str" for string fields

	hostRef      string   // machineRef.name / hostname of the target hosts[] element
	hostPath     []string // sequence path within that host (e.g. devices)
	appendValues []string
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
			case "osd-devices":
				adoptOSDDevices(facet, storage.Cluster, cluster, fileEdits, &summary)
			default:
				for _, object := range facet.Objects {
					summary.Detected = append(summary.Detected, fmt.Sprintf("%s: %s %s (%s) — adjust the YAML manually", storage.Cluster, facet.Name, object.Key, object.State))
				}
			}
		}
		// Filter/all osd hosts are not drift, so they carry no facet object; report
		// each as a reconstruction advisory naming the devices to pin by hand.
		for _, adv := range storage.Report.UnpinnedOSDHosts {
			summary.Detected = append(summary.Detected, fmt.Sprintf("%s: osd host %s consumes [%s] via a filter/all selection — pin osd.dataDevices.paths for exact reconstruction", storage.Cluster, adv.Host, strings.Join(adv.Devices, " ")))
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

// adoptOSDDevices folds real OSD device layout into desired state. The only
// auto-adopt is purely additive: when a host that pins explicit per-host device
// paths has grown OSDs on further devices out of band, those devices are
// appended to its declared list so a rebuild is faithful. A device that
// disappeared (a declared path no longer backing an OSD), a host whose devices
// come from a drivegroup or pathSpecs, and a declared-but-absent host are all
// reported for deliberate hand-editing — never rewritten or removed.
func adoptOSDDevices(facet cephdiff.FacetDiff, clusterName string, cluster v1alpha1.StorageCluster, fileEdits map[string][]nodeEdit, summary *adoptSummary) {
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

// osdDevicesField returns the object's "devices" field.
func osdDevicesField(object cephdiff.ObjectDiff) (cephdiff.FieldDiff, bool) {
	for _, field := range object.Fields {
		if field.Name == "devices" {
			return field, true
		}
	}
	return cephdiff.FieldDiff{}, false
}

// hostDeviceSequencePath returns the desired-state sequence a host's OSD data
// devices are declared under and whether appending to it is safe: the `devices`
// shorthand or osd.dataDevices.paths. A host whose devices come from a covering
// drivegroup or from pathSpecs (per-device CRUSH class) is not safely appendable
// and returns canAppend=false.
func hostDeviceSequencePath(host v1alpha1.StorageCephHost) (path []string, canAppend bool) {
	if len(host.Devices) > 0 {
		return []string{"devices"}, true
	}
	if host.OSD != nil && host.OSD.DataDevices != nil && len(host.OSD.DataDevices.Paths) > 0 {
		return []string{"osd", "dataDevices", "paths"}, true
	}
	return nil, false
}

// subtractFields returns the space-separated tokens of a that are absent from b,
// sorted.
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

// devicePaths maps kernel basenames back to their /dev/<name> device paths, the
// form a plain per-host device list already uses.
func devicePaths(basenames []string) []string {
	out := make([]string, len(basenames))
	for i, name := range basenames {
		out[i] = "/dev/" + name
	}
	return out
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
