package workflow

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestBareMetalFirstInstallClusters(t *testing.T) {
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "owned-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-kubevirt"}},
	}
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "bm"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}},
			{Metadata: v1alpha1.Metadata{Name: "kv"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt}},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "bm-0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bm"}}}},
			{Metadata: v1alpha1.Metadata{Name: "kv-0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}}}},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "fresh-bm"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "bm-0"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "owned-bm"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "bm-0"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "fresh-kubevirt"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "kv-0"}}}}},
		},
	}

	got := BareMetalFirstInstallClusters([]string{"owned-bm"}, tasks, state)
	want := []string{"fresh-bm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BareMetalFirstInstallClusters = %v, want %v", got, want)
	}
}

func TestFirstInstallManagedOSBareMetalClusters(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "bm"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}},
			{Metadata: v1alpha1.Metadata{Name: "kv"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt}},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "bm-0"}, Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}},
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bm"}},
			}},
			{Metadata: v1alpha1.Metadata{Name: "kv-0"}, Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}},
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}},
			}},
		},
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "fresh-ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "bm-0"}}}}}}},
			{Metadata: v1alpha1.Metadata{Name: "owned-ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "bm-0"}}}}}}},
			{Metadata: v1alpha1.Metadata{Name: "fresh-kv-ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "kv-0"}}}}}}},
		},
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindManagedMachineOS, Cluster: "fresh-ceph"}, State: state},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindManagedMachineOS, Cluster: "owned-ceph"}, State: state},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindManagedMachineOS, Cluster: "fresh-kv-ceph"}, State: state},
	}
	objects := []ObjectClassification{
		{Kind: ApplyTaskKindManagedMachineOS, Cluster: "fresh-ceph"},
		{Kind: ApplyTaskKindManagedMachineOS, Cluster: "owned-ceph", counts: map[ConvergeSafetyClassification]int{ConvergeSafetyMatch: 1}},
		{Kind: ApplyTaskKindManagedMachineOS, Cluster: "fresh-kv-ceph"},
	}

	got := FirstInstallManagedOSBareMetalClusters(objects, tasks)
	want := []string{"fresh-ceph"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FirstInstallManagedOSBareMetalClusters = %v, want %v", got, want)
	}
}
