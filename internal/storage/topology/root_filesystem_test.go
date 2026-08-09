package topology

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

func TestResolveNodeMachineProfile(t *testing.T) {
	node := v1alpha1.StorageCephNode{MachineRef: v1alpha1.LocalObjectReference{Name: "node-0"}}
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "node-0"},
			Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "virt"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			}},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "virt"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerLibvirt,
				Libvirt: &v1alpha1.InfraProviderLibvirt{MachineProfiles: []v1alpha1.MachineProfile{{
					Name: "ceph", DiskGiB: 40,
				}}},
			},
		}},
	}
	got, ok := ResolveNodeMachineProfile(state, node)
	if !ok || got.Machine.Metadata.Name != "node-0" || got.Provider.Metadata.Name != "virt" || got.Profile.Name != "ceph" || got.EffectiveDiskGiB != 40 {
		t.Fatalf("ResolveNodeMachineProfile = %+v,%v", got, ok)
	}

	state.InfraProviders[0].Spec.Type = v1alpha1.ProvisionerKubeVirt
	state.InfraProviders[0].Spec.Libvirt = nil
	state.InfraProviders[0].Spec.KubeVirt = &v1alpha1.InfraProviderKubeVirt{MachineProfiles: []v1alpha1.MachineProfile{{Name: "ceph"}}}
	got, ok = ResolveNodeMachineProfile(state, node)
	if !ok || got.Profile.DiskGiB != 0 || got.EffectiveDiskGiB != KubeVirtDefaultDiskGiB {
		t.Fatalf("ResolveNodeMachineProfile KubeVirt default = %+v,%v", got, ok)
	}
}
