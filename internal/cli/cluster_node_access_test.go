package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func accessTestNodes() []stateview.ClusterNode {
	return []stateview.ClusterNode{
		{MachineName: "cp-0", Role: "master", Ordinal: 0, Hostname: "cp-0.managed.test", Kind: stateview.MachineClusterKindContainer},
		{MachineName: "cp-1", Role: "master", Ordinal: 1, Hostname: "cp-1.managed.test", Kind: stateview.MachineClusterKindContainer},
		{MachineName: "w-0", Role: "worker", Ordinal: 0, Hostname: "w-0.managed.test", Kind: stateview.MachineClusterKindContainer},
	}
}

func TestResolveClusterNodeSelectorPrecedence(t *testing.T) {
	cases := []struct{ selector, want string }{
		{"cp-1", "cp-1"},
		{"w-0", "w-0"},
		{"cp-0.managed.test", "cp-0"},
		{"master-1", "cp-1"},
		{"worker-0", "w-0"},
	}
	for _, tc := range cases {
		got, err := resolveClusterNode(accessTestNodes(), tc.selector)
		if err != nil || got != tc.want {
			t.Fatalf("selector %q = %q, %v; want %q", tc.selector, got, err, tc.want)
		}
	}
}

func TestResolveClusterNodeUnknown(t *testing.T) {
	if _, err := resolveClusterNode(accessTestNodes(), "master-9"); err == nil {
		t.Fatal("out-of-range ordinal should not resolve")
	}
	if _, err := resolveClusterNode(accessTestNodes(), "ghost"); err == nil {
		t.Fatal("unknown selector should not resolve")
	}
}

func TestResolveClusterNodeAmbiguousHostname(t *testing.T) {
	nodes := []stateview.ClusterNode{
		{MachineName: "a", Hostname: "dup.example.test", Kind: stateview.MachineClusterKindStorage},
		{MachineName: "b", Hostname: "dup.example.test", Kind: stateview.MachineClusterKindStorage},
	}
	if _, err := resolveClusterNode(nodes, "dup"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous hostname err = %v", err)
	}
}

func TestClusterNodeMachineSNOAutoConnect(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "sno"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "sno-0"}, Role: v1alpha1.NodeRoleMaster},
		}},
	}}}
	m, err := clusterNodeMachine(state, "sno", "")
	if err != nil || m != "sno-0" {
		t.Fatalf("SNO auto-connect = %q, %v", m, err)
	}
}

func TestClusterNodeMachineMultiNodeDefaultsToFirstNode(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ha"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-0"}, Role: v1alpha1.NodeRoleMaster},
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-1"}, Role: v1alpha1.NodeRoleMaster},
		}},
	}}}
	m, err := clusterNodeMachine(state, "ha", "")
	if err != nil || m != "cp-0" {
		t.Fatalf("multi-node empty selector = %q, %v; want the first declared node", m, err)
	}
}

func TestClusterNodeMachineStorageDefaultsToFirstTopologyNode(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-dc1-0"}},
				{Name: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-dc1-1"}},
			}},
		}},
	}}}
	m, err := clusterNodeMachine(state, "ceph", "")
	if err != nil || m != "ceph-dc1-0" {
		t.Fatalf("storage empty selector = %q, %v; want the first declared topology node", m, err)
	}
}

func TestClusterNodeMachineUnknownSelectorListsRoster(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ha"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-0"}, Role: v1alpha1.NodeRoleMaster},
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-1"}, Role: v1alpha1.NodeRoleMaster},
		}},
	}}}
	_, err := clusterNodeMachine(state, "ha", "ghost")
	if err == nil || !strings.Contains(err.Error(), "no node \"ghost\"") || !strings.Contains(err.Error(), "master-1") {
		t.Fatalf("unknown selector err = %v; want the refusal plus the node roster", err)
	}
}
