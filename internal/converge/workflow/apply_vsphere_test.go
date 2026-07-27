package workflow

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func vspherePlanningState() v1alpha1.State {
	return v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab-vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerVSphere,
				VSphere: &v1alpha1.InfraProviderVSphere{
					VCenters: []v1alpha1.VSphereVCenter{{
						Server:         "vcenter.example.test",
						Datacenters:    []string{"dc1"},
						CredentialsRef: v1alpha1.SecretRef{Name: "vcenter-credentials"},
					}},
					FailureDomains: []v1alpha1.VSphereFailureDomain{{
						Name:   "dc1-zone-a",
						Server: "vcenter.example.test",
						Topology: v1alpha1.VSphereFailureTopology{
							Datacenter:     "dc1",
							ComputeCluster: "/dc1/host/cluster1",
							Datastore:      "/dc1/datastore/datastore1",
							Networks:       []string{"lab-portgroup"},
						},
					}},
					MachineProfiles: []v1alpha1.MachineProfile{{Name: "node"}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "master-0"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-vsphere"},
					ProfileRef:  v1alpha1.LocalObjectReference{Name: "node"},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec: v1alpha1.ContainerClusterSpec{
				Nodes: []v1alpha1.OCPNodeSpec{{
					Name:       "master-0",
					Role:       v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
				}},
			},
		}},
	}
}

func vsphereMultiMachinePlanningState() v1alpha1.State {
	state := vspherePlanningState()
	state.Machines = append(state.Machines, v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "master-1"},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-vsphere"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "node"},
			},
		},
	})
	state.ContainerClusters[0].Spec.Nodes = append(state.ContainerClusters[0].Spec.Nodes, v1alpha1.OCPNodeSpec{
		Name:       "master-1",
		Role:       v1alpha1.NodeRoleMaster,
		MachineRef: v1alpha1.LocalObjectReference{Name: "master-1"},
	})
	return state
}

func TestPlanApplyOrdersVSphereMachineTasks(t *testing.T) {
	state := loadWorkflowFixtureState(t, "007-sno-vsphere")

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskResourceKeys(t, tasks, "infra.sno-vsphere.master-0", "vsphere:vcenter.bootwright.test:dc1:vm:sno-vsphere-master-0")
	assertTaskResourceKeys(t, tasks, "boot.sno-vsphere", "vsphere:vcenter.bootwright.test:dc1:vm:sno-vsphere-master-0")
}

func TestVSphereMachineTasksRunOnControllerWithPerVMKeys(t *testing.T) {
	state := vspherePlanningState()
	if got := applyMachineHost(state, "master-0"); got != "localhost" {
		t.Fatalf("applyMachineHost = %q, want localhost", got)
	}
	want := []string{"vsphere:vcenter.example.test:dc1:vm:ocp-master-0"}
	if got := applyMachineExclusiveResourceKeys(state, "ocp", "master-0"); !reflect.DeepEqual(got, want) {
		t.Fatalf("applyMachineExclusiveResourceKeys = %v, want %v", got, want)
	}
	if got := applyNodeBootResourceKeys(state, "ocp", []string{"master-0"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("applyNodeBootResourceKeys = %v, want %v", got, want)
	}
	if applyMachineNeedsSubstratePrepare(state, "master-0") {
		t.Fatal("vsphere machines must not request a substrate prepare phase")
	}
}

func TestVSphereMachineKeysDoNotSerializeAcrossVMsOrClusters(t *testing.T) {
	state := vsphereMultiMachinePlanningState()
	first := applyMachineExclusiveResourceKeys(state, "ocp", "master-0")
	second := applyMachineExclusiveResourceKeys(state, "ocp", "master-1")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected one exclusive key per machine, got %v and %v", first, second)
	}
	if first[0] == second[0] {
		t.Fatalf("machines on one vCenter must not share an exclusive resource key, got %q", first[0])
	}
	other := applyMachineExclusiveResourceKeys(state, "other", "master-0")
	if len(other) != 1 || other[0] == first[0] {
		t.Fatalf("per-cluster VM names must not collide, got %v and %v", other, first)
	}
	boot := applyNodeBootResourceKeys(state, "ocp", []string{"master-0", "master-1"})
	want := []string{first[0], second[0]}
	if !reflect.DeepEqual(boot, want) {
		t.Fatalf("applyNodeBootResourceKeys = %v, want %v", boot, want)
	}
}

func TestVSphereMachineTasksBoundConcurrencyPerVCenter(t *testing.T) {
	state := vsphereMultiMachinePlanningState()
	for _, machineName := range []string{"master-0", "master-1"} {
		if got := applyMachineHostSlotKey(state, machineName); got != "vcenter:vcenter.example.test" {
			t.Fatalf("applyMachineHostSlotKey(%s) = %q, want vcenter:vcenter.example.test", machineName, got)
		}
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "infra.ocp.master-0"}, HostSlotKey: "vcenter:vcenter.example.test", HostSlotCount: 1},
		{Entry: TaskLedgerEntry{ID: "infra.ocp.master-1"}, HostSlotKey: "vcenter:vcenter.example.test", HostSlotCount: 1},
	}
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
	if limits.ParallelismPerHost < 2 {
		t.Fatalf("per-vCenter host slot limit = %d, want at least 2 concurrent clones", limits.ParallelismPerHost)
	}
	running := map[string]int{}
	for _, task := range tasks {
		if !taskHostSlotAvailable(task, running, limits.ParallelismPerHost) {
			t.Fatalf("task %s blocked on its own vCenter slot", task.Entry.ID)
		}
		key, count := taskHostSlot(task)
		running[key] += count
	}
}

func TestVSphereHostSlotKeyOmittedWithoutServer(t *testing.T) {
	state := vspherePlanningState()
	state.InfraProviders[0].Spec.VSphere.FailureDomains = nil
	state.InfraProviders[0].Spec.VSphere.VCenters = nil
	if got := applyMachineHostSlotKey(state, "master-0"); got != "" {
		t.Fatalf("applyMachineHostSlotKey = %q, want empty when no vCenter resolves", got)
	}
}
