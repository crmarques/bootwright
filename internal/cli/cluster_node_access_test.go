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
		{"cp-1", "cp-1"},              // exact Machine name
		{"w-0", "w-0"},                // short hostname (also a Machine name here)
		{"cp-0.managed.test", "cp-0"}, // full FQDN
		{"master-1", "cp-1"},          // role-ordinal
		{"worker-0", "w-0"},           // role-ordinal
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
		Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "sno-0"}, Role: v1alpha1.NodeRoleMaster},
		}},
	}}}
	m, err := clusterNodeMachine(state, "sno", "")
	if err != nil || m != "sno-0" {
		t.Fatalf("SNO auto-connect = %q, %v", m, err)
	}
}

func TestClusterNodeMachineMultiNodeRequiresSelector(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ha"},
		Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-0"}, Role: v1alpha1.NodeRoleMaster},
			{MachineRef: v1alpha1.LocalObjectReference{Name: "cp-1"}, Role: v1alpha1.NodeRoleMaster},
		}},
	}}}
	if _, err := clusterNodeMachine(state, "ha", ""); err == nil || !strings.Contains(err.Error(), "select one with --node") {
		t.Fatalf("multi-node empty selector err = %v", err)
	}
}
