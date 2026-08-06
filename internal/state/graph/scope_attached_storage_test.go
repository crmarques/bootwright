package stategraph

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func attachedStorageServiceState() v1alpha1.State {
	state := storageConsumerState()
	state.Environments = []v1alpha1.Environment{{
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
	}}
	state.InfraComponents = []v1alpha1.InfraComponent{{
		Metadata: v1alpha1.Metadata{Name: "dns"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.InfraComponentTypeDnsmasq,
			NameResolution: &v1alpha1.NameResolutionComponent{
				Implementation: v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
			},
		},
	}}
	state.NetworkConfigs = []v1alpha1.NetworkConfig{{
		Metadata: v1alpha1.Metadata{Name: "storage-network"},
		Spec: v1alpha1.NetworkConfigSpec{
			NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "site-dns"}},
		},
	}}
	cephNode := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	cephNode.Spec.Network.Config.NetworkConfigRef = v1alpha1.LocalObjectReference{Name: "storage-network"}
	state.Machines = []v1alpha1.Machine{
		{Metadata: v1alpha1.Metadata{Name: "ocp-0"}},
		cephNode,
		{Metadata: v1alpha1.Metadata{Name: "bastion"}},
	}
	state.ContainerClusters = []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "ocp-0"}}},
		},
	}}
	state.StorageClusters = []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{
						Name:       "ceph-0",
						MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
					}},
				},
			},
		},
	}}
	return state
}

func machineNameSet(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, machine := range state.Machines {
		out[machine.Metadata.Name] = true
	}
	return out
}

func TestFilterStateToClustersKeepsAttachedStorageServiceHost(t *testing.T) {
	filtered := FilterStateToClusters(attachedStorageServiceState(), []string{"ocp"})

	machines := machineNameSet(filtered)
	if !machines["ceph-0"] {
		t.Fatalf("attached storage node must stay in the filtered state: %v", machines)
	}
	if !machines["bastion"] {
		t.Fatalf("machine hosting a service the attached storage cluster consumes must stay in the filtered state: %v", machines)
	}
}

func TestFilterStateToStorageClustersKeepsAttachedClusterServiceHost(t *testing.T) {
	state := attachedStorageServiceState()
	state.NetworkConfigs[0].Spec.NameResolutionRefs = nil
	ocpNode := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ocp-0"}}
	ocpNode.Spec.Network.Config.NetworkConfigRef = v1alpha1.LocalObjectReference{Name: "cluster-network"}
	state.Machines[0] = ocpNode
	state.NetworkConfigs = append(state.NetworkConfigs, v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: "cluster-network"},
		Spec: v1alpha1.NetworkConfigSpec{
			NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "site-dns"}},
		},
	})

	filtered := FilterStateToStorageClusters(state, []string{"ceph"})

	machines := machineNameSet(filtered)
	if !machines["ocp-0"] {
		t.Fatalf("attached container cluster node must stay in the filtered state: %v", machines)
	}
	if !machines["bastion"] {
		t.Fatalf("machine hosting a service the attached container cluster consumes must stay in the filtered state: %v", machines)
	}
}
