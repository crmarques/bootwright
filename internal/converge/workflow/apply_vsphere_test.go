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
				Hosts: []v1alpha1.OCPHostSpec{{
					Hostname:   "master-0",
					Role:       v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
				}},
			},
		}},
	}
}

// TestPlanApplyOrdersVSphereMachineTasks pins the planned task graph for a
// vCenter-managed cluster: machine infra and boot tasks serialize on the
// per-vCenter resource key and the machine-infra task targets the
// controller-local machine task host.
func TestPlanApplyOrdersVSphereMachineTasks(t *testing.T) {
	state := loadWorkflowFixtureState(t, "007-sno-vsphere")

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskResourceKeys(t, tasks, "infra.sno-vsphere.master-0", "vsphere:vcenter.bootwright.test")
	assertTaskResourceKeys(t, tasks, "boot.sno-vsphere", "vsphere:vcenter.bootwright.test")
}

// TestVSphereMachineTasksRunOnControllerWithPerVCenterKeys pins the planning
// inputs for vCenter-managed machines: tasks run on localhost (the vCenter
// API is driven from the controller) and mutating tasks serialize per
// vCenter server.
func TestVSphereMachineTasksRunOnControllerWithPerVCenterKeys(t *testing.T) {
	state := vspherePlanningState()
	if got := applyMachineHost(state, "master-0"); got != "localhost" {
		t.Fatalf("applyMachineHost = %q, want localhost", got)
	}
	want := []string{"vsphere:vcenter.example.test"}
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
