package stategraph

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachinesWithoutProvisioningWorkFlagsOrphan(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "orphan"}},
			{Metadata: v1alpha1.Metadata{Name: "node-a"}},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ContainerClusterSpec{
				Hosts: []v1alpha1.OCPHostSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "node-a"}}},
			},
		}},
	}
	orphans := MachinesWithoutProvisioningWork(state, []string{"orphan", "node-a"})
	if len(orphans) != 1 || orphans[0] != "orphan" {
		t.Fatalf("MachinesWithoutProvisioningWork = %v, want [orphan]", orphans)
	}
}

func TestMachineWorkObjectsIncludesProviderHost(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "node-a"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "p1"}}}},
			{Metadata: v1alpha1.Metadata{Name: "host-vm"}},
		},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "p1"},
			Spec:     v1alpha1.InfraProviderSpec{Libvirt: &v1alpha1.InfraProviderLibvirt{MachineRef: v1alpha1.LocalObjectReference{Name: "host-vm"}}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ContainerClusterSpec{
				Hosts: []v1alpha1.OCPHostSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "node-a"}}},
			},
		}},
	}
	_, hosts := MachineWorkObjects(state, []string{"node-a"})
	if !hosts["host-vm"] {
		t.Fatalf("MachineWorkObjects closure should include libvirt provider host host-vm, got %v", hosts)
	}
}
