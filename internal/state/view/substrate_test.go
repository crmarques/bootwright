package stateview

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContainerClusterSubstrate(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "metal"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}},
			{Metadata: v1alpha1.Metadata{Name: "kv"}, Spec: v1alpha1.InfraProviderSpec{
				Type:     v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "parent"}},
			}},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "m-metal"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "metal"}}}},
			{Metadata: v1alpha1.Metadata{Name: "m-kv"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}}}},
		},
	}
	metal := v1alpha1.ContainerCluster{Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "m-metal"}}}}}
	child := v1alpha1.ContainerCluster{Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "m-kv"}}}}}

	if got := ContainerClusterSubstrate(state, metal); got.Provider != v1alpha1.ProvisionerBareMetal || got.Host != "" {
		t.Fatalf("metal substrate = %+v, want baremetal/no-host", got)
	}
	if got := ContainerClusterSubstrate(state, child); got.Provider != v1alpha1.ProvisionerKubeVirt || got.Host != "parent" {
		t.Fatalf("child substrate = %+v, want kubevirt/parent", got)
	}
	// A cluster with no resolvable machines yields an empty substrate.
	if got := ContainerClusterSubstrate(state, v1alpha1.ContainerCluster{}); got.Provider != "" {
		t.Fatalf("empty cluster substrate = %+v, want empty", got)
	}
}
