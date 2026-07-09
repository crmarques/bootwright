package cephadopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestComputeEdits(t *testing.T) {
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
	probed := []ProbedStorage{{
		Cluster: "ceph-prod",
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
	}}

	edits, summary, err := ComputeEdits(workspace.Context{InputDir: inputDir}, state, probed)
	if err != nil {
		t.Fatal(err)
	}
	byRel := map[string]string{}
	for _, edit := range edits {
		byRel[edit.RelPath] = string(edit.Content)
	}

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

	cluster, ok := byRel["cluster.yaml"]
	if !ok {
		t.Fatalf("expected an edit to cluster.yaml, got %v", keysOf(byRel))
	}
	if !strings.Contains(cluster, "300") {
		t.Fatalf("config value not updated:\n%s", cluster)
	}

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

func TestAdoptOSDDevices(t *testing.T) {
	inputDir := t.TempDir()
	clusterPath := filepath.Join(inputDir, "cluster.yaml")
	clusterYAML := `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-prod
spec:
  ceph:
    topology:
      hosts:
        - hostname: srv1
          machineRef:
            name: srv1
          roles: [osd]
          devices:
            - /dev/sdb # authored device comment
            - /dev/sdc
        - hostname: srv2
          machineRef:
            name: srv2
          roles: [osd]
          osd:
            dataDevices:
              all: true
`
	if err := os.WriteFile(clusterPath, []byte(clusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	cluster := v1alpha1.StorageCluster{
		Metadata:   v1alpha1.Metadata{Name: "ceph-prod"},
		SourcePath: clusterPath,
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
				{Hostname: "srv1", MachineRef: v1alpha1.LocalObjectReference{Name: "srv1"}, Roles: []string{"osd"}, Devices: []string{"/dev/sdb", "/dev/sdc"}},
				{Hostname: "srv2", MachineRef: v1alpha1.LocalObjectReference{Name: "srv2"}, Roles: []string{"osd"}, OSD: &v1alpha1.StorageCephHostOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}},
			}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	probed := []ProbedStorage{{
		Cluster: "ceph-prod",
		Report: cephdiff.Report{Cluster: "ceph-prod", Probed: true,
			Facets: []cephdiff.FacetDiff{{Name: "osd-devices", Objects: []cephdiff.ObjectDiff{
				{Key: "srv1", State: cephdiff.ObjectChanged, Fields: []cephdiff.FieldDiff{
					{Name: "devices", Desired: "sdb sdc", Real: "sdb sdc sdd", HasDesired: true, HasReal: true},
				}},
			}}},
			UnpinnedOSDHosts: []cephdiff.OSDDeviceAdvisory{{Host: "srv2", Devices: []string{"sdb", "sdc"}}},
		},
	}}

	edits, summary, err := ComputeEdits(workspace.Context{InputDir: inputDir}, state, probed)
	if err != nil {
		t.Fatal(err)
	}
	byRel := map[string]string{}
	for _, edit := range edits {
		byRel[edit.RelPath] = string(edit.Content)
	}
	out, ok := byRel["cluster.yaml"]
	if !ok {
		t.Fatalf("expected an edit to cluster.yaml, got %v", keysOf(byRel))
	}
	for _, want := range []string{"/dev/sdb", "/dev/sdc", "/dev/sdd", "authored device comment"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cluster.yaml missing %q:\n%s", want, out)
		}
	}
	if !anyContains(summary.Applied, "srv1 pin +[sdd]") {
		t.Fatalf("expected srv1 pin in Applied, got %v", summary.Applied)
	}
	if !anyContains(summary.Detected, "srv2") || !anyContains(summary.Detected, "filter/all") {
		t.Fatalf("expected srv2 filter/all advisory in Detected, got %v", summary.Detected)
	}
	if strings.Contains(out, "all: false") || anyContains(summary.Applied, "srv2") {
		t.Fatalf("srv2 filter selection must not be rewritten:\n%s\napplied=%v", out, summary.Applied)
	}
}

func TestSynthesizePoolFileRefusesErasure(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata:   v1alpha1.Metadata{Name: "ceph"},
		SourcePath: "/tmp/ceph/cluster.yaml",
	}
	ecPool := cephdiff.ObjectDiff{
		Key:   "ec-pool",
		State: cephdiff.ObjectRealOnly,
		Fields: []cephdiff.FieldDiff{
			{Name: "type", Real: "erasure", HasReal: true},
			{Name: "erasure_profile", Real: "myprofile", HasReal: true},
			{Name: "application", Real: "rbd", HasReal: true},
		},
	}
	if _, _, err := synthesizePoolFile(cluster, ecPool); err == nil {
		t.Fatal("synthesizePoolFile must refuse an erasure-coded pool, got nil error")
	}

	replPool := cephdiff.ObjectDiff{
		Key:   "rbd-pool",
		State: cephdiff.ObjectRealOnly,
		Fields: []cephdiff.FieldDiff{
			{Name: "type", Real: "replicated", HasReal: true},
			{Name: "size", Real: "3", HasReal: true},
			{Name: "min_size", Real: "2", HasReal: true},
			{Name: "application", Real: "rbd", HasReal: true},
		},
	}
	path, content, err := synthesizePoolFile(cluster, replPool)
	if err != nil {
		t.Fatalf("synthesizePoolFile(replicated) failed: %v", err)
	}
	if !strings.HasSuffix(path, "rbd-pool.yaml") {
		t.Fatalf("unexpected synthesized path %q", path)
	}
	if !strings.Contains(string(content), "type: replicated") {
		t.Fatalf("synthesized replicated pool missing type: replicated:\n%s", content)
	}
}

func anyContains(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
