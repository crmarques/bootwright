package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageFIPSRequirementReachesClusterAndEveryHost(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Security: v1alpha1.StorageCephSecurity{FIPS: v1alpha1.StorageCephFIPS{Enabled: true}},
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "machine-01"}},
				{Name: "node-02", MachineRef: v1alpha1.LocalObjectReference{Name: "machine-02"}},
			}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	clusters := storageClustersVars(state, PathOptions{})
	if len(clusters) != 1 {
		t.Fatalf("storage clusters = %d, want 1", len(clusters))
	}
	entry := clusters[0].(map[string]any)
	if got := entry["fips"]; got != true {
		t.Fatalf("storage cluster fips = %v, want true", got)
	}
	hosts := entry["hosts"].([]any)
	if len(hosts) != 2 {
		t.Fatalf("storage hosts = %d, want 2", len(hosts))
	}
	for _, raw := range hosts {
		host := raw.(map[string]any)
		if got := host["fipsRequired"]; got != true {
			t.Fatalf("storage host %v fipsRequired = %v, want true", host["hostname"], got)
		}
	}
}

func TestStorageFIPSRequirementIsOmittedWhenDisabled(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "machine-01"},
			}}},
		}},
	}
	entry := storageClustersVars(v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}, PathOptions{})[0].(map[string]any)
	if _, ok := entry["fips"]; ok {
		t.Fatalf("disabled storage cluster carries fips: %v", entry["fips"])
	}
	host := entry["hosts"].([]any)[0].(map[string]any)
	if _, ok := host["fipsRequired"]; ok {
		t.Fatalf("disabled storage host carries fipsRequired: %v", host["fipsRequired"])
	}
}
