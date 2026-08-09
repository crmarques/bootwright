package ceph

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func TestPrometheusSpecAlwaysBoundsRetentionSize(t *testing.T) {
	node := v1alpha1.StorageCephNode{
		Name:       "ceph-0",
		MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
		Roles:      []string{"mon", "prometheus"},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{node}},
		}},
	}
	for _, doc := range cephadmMonitoringSpecs(cluster) {
		spec := doc.(map[string]any)
		if spec["service_type"] != "prometheus" {
			continue
		}
		if got := spec["spec"].(map[string]any)["retention_size"]; got != topology.PrometheusDefaultRetentionSize {
			t.Fatalf("prometheus retention_size = %v, want %q", got, topology.PrometheusDefaultRetentionSize)
		}
		return
	}
	t.Fatal("no prometheus spec rendered")
}
