package stateview

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterNodesContainerOrdinals(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ha"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-0"}, Role: v1alpha1.NodeRoleMaster, Name: "cp-0.ha.test"},
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-1"}, Role: v1alpha1.NodeRoleMaster},
			{MachineRef: v1alpha1.LocalObjectReference{Name: "w-0"}, Role: v1alpha1.NodeRoleWorker},
		}},
	}}}
	nodes, ok := ClusterNodes(state, "ha")
	if !ok || len(nodes) != 3 {
		t.Fatalf("ClusterNodes = %+v, ok=%v", nodes, ok)
	}
	if nodes[0].Ordinal != 0 || nodes[1].Ordinal != 1 || nodes[2].Ordinal != 0 {
		t.Fatalf("ordinals = %d,%d,%d", nodes[0].Ordinal, nodes[1].Ordinal, nodes[2].Ordinal)
	}
	if nodes[0].Kind != MachineClusterKindContainer || nodes[0].Hostname != "cp-0.ha.test" {
		t.Fatalf("node[0] = %+v", nodes[0])
	}
}

func TestClusterNodesStorageRoles(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"mon", "mgr"}, Name: "ceph-0.ceph.test"},
				{MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Roles: []string{"osd"}},
			}},
		}},
	}}}
	nodes, ok := ClusterNodes(state, "ceph")
	if !ok || len(nodes) != 2 {
		t.Fatalf("ClusterNodes = %+v, ok=%v", nodes, ok)
	}
	if nodes[0].Kind != MachineClusterKindStorage || !nodes[0].HasRole("mon") || !nodes[0].HasRole("mgr") {
		t.Fatalf("node[0] roles = %+v", nodes[0])
	}
	if nodes[1].HasRole("mon") || !nodes[1].HasRole("osd") {
		t.Fatalf("node[1] roles = %+v", nodes[1])
	}
}

func TestClusterNodesUnknownCluster(t *testing.T) {
	if _, ok := ClusterNodes(v1alpha1.State{}, "ghost"); ok {
		t.Fatal("unknown cluster should return ok=false")
	}
}
