package workflow

import (
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func sharedNameResolutionState(consumers ...string) v1alpha1.State {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "site-dns",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "dns"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "dns"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.InfraComponentTypeDnsmasq,
				NameResolution: &v1alpha1.NameResolutionComponent{
					Implementation: v1alpha1.InfraComponentTypeDnsmasq,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
				},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "storage-network"},
			Spec: v1alpha1.NetworkConfigSpec{
				NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "site-dns"}},
			},
		}},
		Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "bastion"}}},
	}
	for _, name := range consumers {
		node := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: name + "-0"}}
		node.Spec.Network.Config.NetworkConfigRef = v1alpha1.LocalObjectReference{Name: "storage-network"}
		state.Machines = append(state.Machines, node)
		state.StorageClusters = append(state.StorageClusters, v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{
							Name:       name + "-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: name + "-0"},
						}},
					},
				},
			},
		})
	}
	return state
}

func TestInfraComponentDestroyScopeRecordsCoversFullyScopedService(t *testing.T) {
	state := sharedNameResolutionState("ceph-a")

	got := InfraComponentDestroyScopeRecords(state, []string{"ceph-a"})

	if want := []string{"InfraComponent-dns"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
}

func TestInfraComponentDestroyScopeRecordsSkipsServiceWithUnscopedConsumer(t *testing.T) {
	state := sharedNameResolutionState("ceph-a", "ceph-b")

	if got := InfraComponentDestroyScopeRecords(state, []string{"ceph-a"}); len(got) != 0 {
		t.Fatalf("a service another cluster still consumes must stay out of the destroy scope, got %v", got)
	}
}

func TestInfraComponentDestroyScopeRecordsSkipsServiceWithoutConsumers(t *testing.T) {
	state := sharedNameResolutionState()

	if got := InfraComponentDestroyScopeRecords(state, []string{"ceph-a"}); len(got) != 0 {
		t.Fatalf("a service no scoped cluster consumes must stay out of the destroy scope, got %v", got)
	}
}

func TestPlanDestroyTasksDropsSharedServiceStepsOutOfClusterScope(t *testing.T) {
	extra := []string{DestroyClusterScopeExtraVar + "=ocp"}

	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, "", extra, nil)
	if err != nil {
		t.Fatal(err)
	}

	ids := destroyTaskIDs(tasks)
	for _, id := range []string{destroyInfraComponentsTaskID, destroyProviderServicesTaskID} {
		if slices.Contains(ids, id) {
			t.Fatalf("task %q must not run for a cluster scope that owns no such services: %v", id, ids)
		}
	}
}

func TestPlanDestroyTasksKeepsSharedServiceStepsForOwnedServices(t *testing.T) {
	extra := []string{DestroyClusterScopeExtraVar + "=ceph-a"}

	tasks, err := PlanDestroyTasks("all", sharedNameResolutionState("ceph-a"), "", extra, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(destroyTaskIDs(tasks), destroyInfraComponentsTaskID) {
		t.Fatalf("task %q must run when the scoped state hosts infra component services: %v", destroyInfraComponentsTaskID, destroyTaskIDs(tasks))
	}
}

func TestPlanDestroyTasksKeepsSharedServiceStepsWithoutClusterScope(t *testing.T) {
	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	ids := destroyTaskIDs(tasks)
	for _, id := range []string{destroyInfraComponentsTaskID, destroyProviderServicesTaskID} {
		if !slices.Contains(ids, id) {
			t.Fatalf("an unscoped destroy sweeps recorded resources, so task %q must run: %v", id, ids)
		}
	}
}
