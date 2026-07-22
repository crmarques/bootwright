package ceph

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func rootFSCluster(nodes []v1alpha1.StorageCephNode, monitoring *v1alpha1.StorageCephMonitoring) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Monitoring: monitoring,
			Topology:   v1alpha1.StorageCephTopology{Nodes: nodes},
		}},
	}
}

func TestNodeRootFilesystemGiBScalesWithRoles(t *testing.T) {
	osdOnly := v1alpha1.StorageCephNode{Name: "ceph-1", Roles: []string{"osd"}}
	full := v1alpha1.StorageCephNode{
		Name:  "ceph-0",
		Roles: []string{"mon", "mgr", "osd", "prometheus", "grafana", "alertmanager"},
	}
	cluster := rootFSCluster([]v1alpha1.StorageCephNode{full, osdOnly}, nil)

	if got := NodeRootFilesystemGiB(cluster, osdOnly); got != 20 {
		t.Fatalf("osd-only node budget = %d GiB, want 20", got)
	}
	fullBudget := NodeRootFilesystemGiB(cluster, full)
	if fullBudget != 20+15+5+14+2+1 {
		t.Fatalf("full-role node budget = %d GiB, want %d", fullBudget, 20+15+5+14+2+1)
	}
	if fullBudget <= RootFilesystemFloorGiB {
		t.Fatalf("full-role budget %d GiB must exceed the %d GiB floor", fullBudget, RootFilesystemFloorGiB)
	}
}

func TestNodeRootFilesystemGiBTracksRetentionSize(t *testing.T) {
	node := v1alpha1.StorageCephNode{Name: "ceph-0", Roles: []string{"prometheus"}}
	cluster := rootFSCluster([]v1alpha1.StorageCephNode{node}, &v1alpha1.StorageCephMonitoring{
		Prometheus: &v1alpha1.StorageCephMonitoringService{RetentionSize: "50GB"},
	})
	if got := NodeRootFilesystemGiB(cluster, node); got != 20+47+4 {
		t.Fatalf("prometheus node budget = %d GiB, want %d", got, 20+47+4)
	}
}

func TestNodeRootFilesystemGiBIgnoresDisabledMonitoring(t *testing.T) {
	node := v1alpha1.StorageCephNode{Name: "ceph-0", Roles: []string{"prometheus", "grafana"}}
	disabled := false
	cluster := rootFSCluster([]v1alpha1.StorageCephNode{node}, &v1alpha1.StorageCephMonitoring{Enabled: &disabled})
	if got := NodeRootFilesystemGiB(cluster, node); got != 20 {
		t.Fatalf("budget with monitoring disabled = %d GiB, want 20", got)
	}
}

func TestNodeRootFilesystemGiBCountsExplicitlyPlacedLoki(t *testing.T) {
	node := v1alpha1.StorageCephNode{Name: "ceph-0", Roles: []string{"osd"}}
	cluster := rootFSCluster([]v1alpha1.StorageCephNode{node}, &v1alpha1.StorageCephMonitoring{
		Loki: &v1alpha1.StorageCephMonitoringService{
			Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-0"}},
		},
	})
	if got := NodeRootFilesystemGiB(cluster, node); got != 40 {
		t.Fatalf("budget with loki placed = %d GiB, want 40", got)
	}
}

func TestPrometheusRetentionSizeDefaultsAndOverrides(t *testing.T) {
	bare := rootFSCluster(nil, nil)
	if got := PrometheusRetentionSize(bare); got != PrometheusDefaultRetentionSize {
		t.Fatalf("default retention size = %q, want %q", got, PrometheusDefaultRetentionSize)
	}
	authored := rootFSCluster(nil, &v1alpha1.StorageCephMonitoring{
		Prometheus: &v1alpha1.StorageCephMonitoringService{RetentionSize: "1TB"},
	})
	if got := PrometheusRetentionSize(authored); got != "1TB" {
		t.Fatalf("authored retention size = %q, want 1TB", got)
	}
}

func TestParseRetentionSizeGiB(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"10GB", 10, true},
		{"10GiB", 10, true},
		{"512MB", 1, true},
		{"1TB", 932, true},
		{" 2 GB ", 2, true},
		{"0GB", 0, false},
		{"", 0, false},
		{"lots", 0, false},
		{"10ZB", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRetentionSizeGiB(tc.in)
		if ok != tc.ok || (tc.ok && got != tc.want) {
			t.Fatalf("parseRetentionSizeGiB(%q) = %d,%v want %d,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPrometheusSpecAlwaysBoundsRetentionSize(t *testing.T) {
	node := v1alpha1.StorageCephNode{
		Name:       "ceph-0",
		MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
		Roles:      []string{"mon", "prometheus"},
	}
	cluster := rootFSCluster([]v1alpha1.StorageCephNode{node}, nil)
	for _, doc := range cephadmMonitoringSpecs(cluster) {
		spec := doc.(map[string]any)
		if spec["service_type"] != "prometheus" {
			continue
		}
		if got := spec["spec"].(map[string]any)["retention_size"]; got != PrometheusDefaultRetentionSize {
			t.Fatalf("prometheus retention_size = %v, want %q", got, PrometheusDefaultRetentionSize)
		}
		return
	}
	t.Fatal("no prometheus spec rendered")
}
