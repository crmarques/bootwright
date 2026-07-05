package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestComputeAdoptEdits(t *testing.T) {
	inputDir := t.TempDir()
	poolPath := filepath.Join(inputDir, "pools", "rbd.yaml")
	if err := os.MkdirAll(filepath.Dir(poolPath), 0o755); err != nil {
		t.Fatal(err)
	}
	poolYAML := `apiVersion: bootwright.io/v1alpha1
kind: StoragePool
metadata:
  name: rbd
spec:
  storageClusterRef:
    name: ceph-prod
  ceph:
    type: replicated
    role: rbd
    replicated:
      size: 3 # authored size comment
      minSize: 2
`
	if err := os.WriteFile(poolPath, []byte(poolYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	clusterPath := filepath.Join(inputDir, "cluster.yaml")
	clusterYAML := `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-prod
spec:
  ceph:
    config:
      global:
        mon_max_pg_per_osd: "250"
    topology:
      hosts: []
`
	if err := os.WriteFile(clusterPath, []byte(clusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-prod"}, SourcePath: clusterPath, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}},
		},
		StoragePools: []v1alpha1.StoragePool{
			{Metadata: v1alpha1.Metadata{Name: "rbd"}, SourcePath: poolPath, Spec: v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph-prod"}}},
		},
	}
	live := liveDiffReport{
		Storage: []liveStorageDiff{{
			Cluster: "ceph-prod", Probed: true, InSync: false,
			Report: cephdiff.Report{Cluster: "ceph-prod", Probed: true, Facets: []cephdiff.FacetDiff{
				{Name: "pools", Objects: []cephdiff.ObjectDiff{
					{Key: "rbd", State: cephdiff.ObjectChanged, Fields: []cephdiff.FieldDiff{
						{Name: "size", Desired: "3", Real: "2", HasDesired: true, HasReal: true},
					}},
					{Key: "extra", State: cephdiff.ObjectRealOnly, Fields: []cephdiff.FieldDiff{
						{Name: "type", Real: "replicated", HasReal: true},
						{Name: "size", Real: "3", HasReal: true},
						{Name: "application", Real: "rbd", HasReal: true},
					}},
				}},
				{Name: "config", Objects: []cephdiff.ObjectDiff{
					{Key: "global/mon_max_pg_per_osd", State: cephdiff.ObjectChanged, Fields: []cephdiff.FieldDiff{
						{Name: "value", Desired: "250", Real: "300", HasDesired: true, HasReal: true},
					}},
				}},
			}},
		}},
	}

	edits, summary, err := computeAdoptEdits(workspace.Context{InputDir: inputDir}, state, live)
	if err != nil {
		t.Fatal(err)
	}
	byRel := map[string]string{}
	for _, edit := range edits {
		byRel[edit.RelPath] = string(edit.Content)
	}

	// Pool size updated surgically, authored comment preserved.
	pool, ok := byRel["pools/rbd.yaml"]
	if !ok {
		t.Fatalf("expected an edit to pools/rbd.yaml, got %v", keysOf(byRel))
	}
	if !strings.Contains(pool, "size: 2") {
		t.Fatalf("pool size not updated:\n%s", pool)
	}
	if !strings.Contains(pool, "authored size comment") {
		t.Fatalf("authored comment lost:\n%s", pool)
	}
	if strings.Contains(pool, "size: 3") {
		t.Fatalf("stale size left in pool file:\n%s", pool)
	}

	// Config value updated in the cluster file.
	cluster, ok := byRel["cluster.yaml"]
	if !ok {
		t.Fatalf("expected an edit to cluster.yaml, got %v", keysOf(byRel))
	}
	if !strings.Contains(cluster, "300") {
		t.Fatalf("config value not updated:\n%s", cluster)
	}

	// New pool synthesized as a sibling file.
	extra, ok := byRel["extra.yaml"]
	if !ok {
		t.Fatalf("expected a new extra.yaml, got %v", keysOf(byRel))
	}
	for _, want := range []string{"kind: StoragePool", "name: extra", "storageClusterRef: ceph-prod"} {
		if !strings.Contains(extra, want) {
			t.Fatalf("synthesized pool missing %q:\n%s", want, extra)
		}
	}

	if len(summary.Applied) != 2 {
		t.Fatalf("expected 2 applied edits, got %v", summary.Applied)
	}
	if len(summary.NewFiles) != 1 {
		t.Fatalf("expected 1 new file, got %v", summary.NewFiles)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
