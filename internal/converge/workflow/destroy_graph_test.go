package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestDestroyChainResolvesBaseAndFannedDependencies(t *testing.T) {
	steps := []destroyStep{
		{id: "destroy.leaf.a", baseID: "destroy.leaf", kind: DestroyTaskKindInfraComponents, label: "Leaf a", playbook: "leaf.yml"},
		{id: "destroy.leaf.b", baseID: "destroy.leaf", kind: DestroyTaskKindInfraComponents, label: "Leaf b", playbook: "leaf.yml"},
		{
			id:                   "destroy.root",
			kind:                 DestroyTaskKindProviderServices,
			label:                "Root",
			playbook:             "root.yml",
			dependencies:         []string{"destroy.leaf"},
			orderingDependencies: []string{"destroy.leaf.a"},
		},
	}
	tasks, err := destroyChain(v1alpha1.State{}, "limit", nil, steps, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := destroyTaskByID(t, tasks, "destroy.root")
	wantHard := []string{"destroy.leaf.a", "destroy.leaf.b"}
	if !reflect.DeepEqual(root.Entry.Dependencies, wantHard) {
		t.Fatalf("hard dep naming the base ID = %v, want the whole fanned set %v", root.Entry.Dependencies, wantHard)
	}
	wantOrdering := []string{"destroy.leaf.a"}
	if !reflect.DeepEqual(root.Entry.OrderingDependencies, wantOrdering) {
		t.Fatalf("edge naming a concrete fanned ID = %v, want %v; an unresolved fanned ID fails OPEN, so a safety edge would vanish with no symptom", root.Entry.OrderingDependencies, wantOrdering)
	}
}

func TestDestroyChainRejectsDependencyCycle(t *testing.T) {
	steps := []destroyStep{
		{id: "destroy.a", kind: DestroyTaskKindInfraComponents, label: "A", playbook: "a.yml", dependencies: []string{"destroy.b"}},
		{id: "destroy.b", kind: DestroyTaskKindProviderServices, label: "B", playbook: "b.yml", orderingDependencies: []string{"destroy.a"}},
	}
	_, err := destroyChain(v1alpha1.State{}, "limit", nil, steps, nil)
	if err == nil {
		t.Fatal("a destroy dependency cycle must fail planning: the scheduler breaks on `running == 0 && !startedAny` with no diagnostic, leaving every task Pending and withholding substrate releases fleet-wide")
	}
	for _, want := range []string{"destroy.a", "destroy.b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("cycle error %q must name the cycle members", err)
		}
	}
}

func TestKubeVirtHostParentsByChildCoversStorageClusters(t *testing.T) {
	state := destroyKubeVirtDependencyState("child", "host")
	state.StorageClusters = []v1alpha1.StorageCluster{
		{
			Metadata: v1alpha1.Metadata{Name: "ceph-tenant"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "child-m0"}}},
					},
				},
			},
		},
	}
	parents := KubeVirtHostParentsByChild(state)
	if !parents["ceph-tenant"]["host"] {
		t.Fatalf("StorageCluster parents = %v; a Ceph cluster whose nodes are KubeVirt VMs must carry a guest->host edge, or it is levelled alongside its own host and the host is destroyed underneath it", parents["ceph-tenant"])
	}
	if !parents["child"]["host"] {
		t.Fatalf("ContainerCluster parents = %v, want host", parents["child"])
	}
}

func TestMachineInfraDestroyLevelsOrderStorageTenantBeforeHost(t *testing.T) {
	state := destroyKubeVirtDependencyState("child", "host")
	state.StorageClusters = []v1alpha1.StorageCluster{
		{
			Metadata: v1alpha1.Metadata{Name: "ceph-tenant"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "child-m0"}}},
					},
				},
			},
		},
	}
	levels, err := machineInfraDestroyLevels(state)
	if err != nil {
		t.Fatal(err)
	}
	levelOf := map[string]int{}
	for i, level := range levels {
		for _, name := range level {
			levelOf[name] = i
		}
	}
	if levelOf["ceph-tenant"] >= levelOf["host"] {
		t.Fatalf("levels = %v; the KubeVirt-backed storage tenant must tear down strictly before its host", levels)
	}
}
