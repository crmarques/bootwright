package workflow

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
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
			successDependencies:  []string{"destroy.leaf"},
			orderingDependencies: []string{"destroy.leaf.a"},
		},
	}
	tasks, err := destroyChain(v1alpha1.State{}, "limit", nil, steps)
	if err != nil {
		t.Fatal(err)
	}
	root := destroyTaskByID(t, tasks, "destroy.root")
	wantHard := []string{"destroy.leaf.a", "destroy.leaf.b"}
	if !reflect.DeepEqual(root.Entry.Dependencies, wantHard) {
		t.Fatalf("hard dep naming the base ID = %v, want the whole fanned set %v", root.Entry.Dependencies, wantHard)
	}
	if !reflect.DeepEqual(root.Entry.SuccessDependencies, wantHard) {
		t.Fatalf("success dep naming the base ID = %v, want the whole fanned set %v", root.Entry.SuccessDependencies, wantHard)
	}
	wantOrdering := []string{"destroy.leaf.a"}
	if !reflect.DeepEqual(root.Entry.OrderingDependencies, wantOrdering) {
		t.Fatalf("edge naming a concrete fanned ID = %v, want %v; an unresolved fanned ID fails OPEN, so a safety edge would vanish with no symptom", root.Entry.OrderingDependencies, wantOrdering)
	}
}

func TestDestroyChainRejectsDependencyCycle(t *testing.T) {
	steps := []destroyStep{
		{id: "destroy.a", kind: DestroyTaskKindInfraComponents, label: "A", playbook: "a.yml", successDependencies: []string{"destroy.b"}},
		{id: "destroy.b", kind: DestroyTaskKindProviderServices, label: "B", playbook: "b.yml", orderingDependencies: []string{"destroy.a"}},
	}
	_, err := destroyChain(v1alpha1.State{}, "limit", nil, steps)
	if err == nil {
		t.Fatal("a destroy dependency cycle must fail planning: the scheduler breaks on `running == 0 && !startedAny` with no diagnostic, leaving every task Pending and withholding substrate releases fleet-wide")
	}
	for _, want := range []string{"destroy.a", "destroy.b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("cycle error %q must name the cycle members", err)
		}
	}
}

func TestFannedMachineInfraScopesItselfAndKeepsTheSweepTerminal(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	sweep := InfraDestroyContextSweepExtraVar + "=true"
	tasks, err := PlanDestroyTasks("all", state, "", []string{sweep}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fanned []ApplyTask
	var records *ApplyTask
	for i, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		if task.Entry.ID == destroyMachineInfraRecordsTaskID {
			records = &tasks[i]
			continue
		}
		if task.Entry.ID != destroyMachineInfraTaskID {
			fanned = append(fanned, task)
		}
	}
	if len(fanned) < 2 || records == nil {
		t.Skipf("fixture does not fan machine teardown: %v", destroyTaskIDs(tasks))
	}
	for _, task := range fanned {
		cluster := strings.TrimPrefix(task.Entry.ID, destroyMachineInfraTaskID+".")
		if !slices.Contains(task.ExtraVarPairs, DestroyClusterScopeExtraVar+"="+cluster) {
			t.Fatalf("fanned task %q must replace the cluster scope with its own cluster: task_machine_infra_destroy.yml gates play 2's ownership-record loop on it alone, so an unscoped fanned task virsh-undefines every other cluster's recorded domains and deletes their disks; got %v", task.Entry.ID, task.ExtraVarPairs)
		}
		if slices.Contains(task.ExtraVarPairs, sweep) {
			t.Fatalf("fanned task %q must not carry the context sweep; only the terminal records task may sweep, got %v", task.Entry.ID, task.ExtraVarPairs)
		}
	}
	if !slices.Contains(records.ExtraVarPairs, sweep) {
		t.Fatalf("the context sweep has exactly one consumer, task_machine_infra_destroy.yml play 3, so it must survive on the terminal records task or it silently stops running; got %v", records.ExtraVarPairs)
	}
	if !slices.Contains(records.ExtraVarPairs, MachineInfraRecordsOnlyExtraVar+"=true") {
		t.Fatalf("the terminal task re-runs the whole playbook, so it must declare records-only or it re-authorizes substrate removal for a cluster whose own task failed; got %v", records.ExtraVarPairs)
	}
	if slices.Contains(records.ExtraVarPairs, DestroyClusterScopeExtraVar+"=") {
		t.Fatalf("the records sweep must carry no cluster scope, or records with a blank or deleted cluster are never reaped: %v", records.ExtraVarPairs)
	}
	wantRecordDependencies := destroyTaskIDsOfKind(fanned)
	if !reflect.DeepEqual(records.Entry.Dependencies, wantRecordDependencies) {
		t.Fatalf("the records sweep must hard-depend on every fanned machine teardown: taskTerminal releases ordering deps on Blocked, which would let the sweep bypass the graph's fail-closed edges; got %v", records.Entry.Dependencies)
	}
	if !reflect.DeepEqual(records.Entry.SuccessDependencies, []string{destroyControllerNameResolutionPreflightTaskID}) {
		t.Fatalf("the records sweep success dependencies = %v, want controller preflight", records.Entry.SuccessDependencies)
	}
}

func destroyTaskIDsOfKind(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.ID)
	}
	return out
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
