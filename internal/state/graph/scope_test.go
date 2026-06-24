package stategraph

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ApplyWorkObjects must report only the objects a scoped apply provisions: the
// selected container cluster's nodes, but neither the data-foundation-attached
// managed StorageCluster nor its nodes, which are render references pulled in
// for the attachment only.
func TestApplyWorkObjectsExcludesRenderReferenceStorage(t *testing.T) {
	state := dataFoundationAttachmentState()

	machines, storageClusters := ApplyWorkObjects(state, []string{"ocp"}, nil)

	if !machines["ocp-0"] {
		t.Errorf("selected container cluster node ocp-0 must be a work machine: %v", machines)
	}
	for _, node := range []string{"ceph-0", "ceph-arb"} {
		if machines[node] {
			t.Errorf("render-reference Ceph node %s must not be a work machine: %v", node, machines)
		}
	}
	if storageClusters["ceph"] {
		t.Errorf("render-reference StorageCluster ceph must not be a work object: %v", storageClusters)
	}
	if len(storageClusters) != 0 {
		t.Errorf("container-only selection has no storage work objects, got %v", storageClusters)
	}
}

// Co-selecting the storage cluster makes it (and its nodes) work objects again.
func TestApplyWorkObjectsIncludesCoSelectedStorage(t *testing.T) {
	state := dataFoundationAttachmentState()

	machines, storageClusters := ApplyWorkObjects(state, []string{"ocp"}, []string{"ceph"})

	if !storageClusters["ceph"] {
		t.Errorf("co-selected StorageCluster ceph must be a work object: %v", storageClusters)
	}
	for _, node := range []string{"ocp-0", "ceph-0", "ceph-arb"} {
		if !machines[node] {
			t.Errorf("node %s must be a work machine when its cluster is selected: %v", node, machines)
		}
	}
}

func dataFoundationAttachmentState() v1alpha1.State {
	node := func(name string) v1alpha1.Machine {
		return v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: name}}
	}
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec: v1alpha1.ContainerClusterSpec{
				Hosts: []v1alpha1.OCPHostSpec{{
					MachineRef: v1alpha1.LocalObjectReference{Name: "ocp-0"},
				}},
			},
		}},
		Machines: []v1alpha1.Machine{node("ocp-0"), node("ceph-0"), node("ceph-arb")},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{
							{Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}},
							{Hostname: "ceph-arb", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-arb"}},
						},
					},
				},
			},
		}},
	}
}
