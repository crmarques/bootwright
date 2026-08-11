package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func TestCephStateDiscoveryLimitTargetsOnlyTheSelectedSeedAcrossTwoClusters(t *testing.T) {
	first := stateDiscoveryTestCluster("ceph-a", "node-a", "machine-a")
	second := stateDiscoveryTestCluster("ceph-b", "node-b", "machine-b")
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{second, first}}

	got, err := cephStateDiscoveryLimit(state, []string{first.Metadata.Name})
	if err != nil {
		t.Fatal(err)
	}
	want := render.StorageSeedHostName(first)
	if got != want {
		t.Fatalf("discovery limit = %q, want selected seed %q", got, want)
	}
	if strings.Contains(got, render.StorageSeedHostName(second)) || strings.Contains(got, render.GroupStorageHosts) {
		t.Fatalf("discovery limit widened beyond the selected cluster: %q", got)
	}
}

func TestCephStateDiscoveryLimitFailsClosedOnAnUnresolvedSelection(t *testing.T) {
	managed := stateDiscoveryTestCluster("ceph-a", "node-a", "machine-a")
	external := stateDiscoveryTestCluster("ceph-external", "node-x", "machine-x")
	external.Spec.Management = v1alpha1.StorageClusterManagementExternal
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{managed, external}}

	for _, tc := range []struct {
		name      string
		selection []string
	}{
		{name: "empty"},
		{name: "blank", selection: []string{" "}},
		{name: "unknown", selection: []string{"ceph-missing"}},
		{name: "external", selection: []string{external.Metadata.Name}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if limit, err := cephStateDiscoveryLimit(state, tc.selection); err == nil {
				t.Fatalf("limit = %q, want fail-closed selection error", limit)
			}
		})
	}
}

func stateDiscoveryTestCluster(name, node, machine string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementManaged,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: node}},
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
					Name:       node,
					MachineRef: v1alpha1.LocalObjectReference{Name: machine},
				}}},
			},
		},
	}
}
