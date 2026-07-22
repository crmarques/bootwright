package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func attachedManagedOSState() v1alpha1.State {
	state := bareMetalManagedOSState()
	state.ContainerClusters = []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{{
				MachineRef: v1alpha1.LocalObjectReference{Name: "ocp-node"},
			}},
		},
	}}
	state.Machines = append(state.Machines, v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "ocp-node"},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bare-metal"}},
			OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
		},
	})
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "ceph-export"},
		Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph-bm"}},
	}}
	state.ClusterAddons = []v1alpha1.ClusterAddon{{
		Metadata: v1alpha1.Metadata{Name: "odf-consumer"},
		Spec: v1alpha1.ClusterAddonSpec{
			Accepts: v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
				Name:        "storage",
				ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
				Effects: []v1alpha1.ClusterAddonInputEffect{{
					StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
				}},
			}}},
		},
	}}
	state.ClusterAddonBindings = []v1alpha1.ClusterAddonBinding{{
		Spec: v1alpha1.ClusterAddonBindingSpec{
			ClusterRef: v1alpha1.LocalObjectReference{Name: "ocp"},
			AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf-consumer"}},
			AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
				AddonRef: v1alpha1.LocalObjectReference{Name: "odf-consumer"},
				Inputs: []v1alpha1.ClusterAddonBindingInput{{
					Name:  "storage",
					Value: "ceph-export",
				}},
			}},
		},
	}}
	return state
}

func managedOSHashes(t *testing.T, renderState, hashState v1alpha1.State) (desired, structural string) {
	t.Helper()
	target := ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}
	tasks, err := PlanApplyTasksCheckedWithHashState(target, renderState, hashState)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for i := range tasks {
		if tasks[i].Entry.Kind == ApplyTaskKindManagedMachineOS && tasks[i].Entry.Cluster == "ceph-bm" {
			d, err := ApplyTaskDesiredHash(tasks[i])
			if err != nil {
				t.Fatalf("desired hash: %v", err)
			}
			s, err := ApplyTaskStructuralHash(tasks[i])
			if err != nil {
				t.Fatalf("structural hash: %v", err)
			}
			return d, s
		}
	}
	t.Fatalf("no managed-OS task for ceph-bm in %v", taskKinds(tasks))
	return "", ""
}

func TestManagedOSHashIsScopeInvariantWithFullHashState(t *testing.T) {
	full := attachedManagedOSState()
	scoped := stategraph.FilterStateToApplyClusterRoots(full, nil, []string{"ceph-bm"})

	wholeClusterDesired, wholeClusterStructural := managedOSHashes(t, full, full)
	clustersScopedDesired, clustersScopedStructural := managedOSHashes(t, scoped, full)

	if wholeClusterDesired != clustersScopedDesired {
		t.Errorf("managed-OS desired hash depends on the render scope: whole-cluster=%s --clusters=%s", wholeClusterDesired, clustersScopedDesired)
	}
	if wholeClusterStructural != clustersScopedStructural {
		t.Errorf("managed-OS structural hash depends on the render scope: whole-cluster=%s --clusters=%s", wholeClusterStructural, clustersScopedStructural)
	}
}

func TestManagedOSClustersScopedHashMatchesRecordedWholeClusterHash(t *testing.T) {
	full := attachedManagedOSState()
	scoped := stategraph.FilterStateToApplyClusterRoots(full, nil, []string{"ceph-bm"})

	recordedDesired, recordedStructural := managedOSHashes(t, full, full)
	clustersScopedDesired, clustersScopedStructural := managedOSHashes(t, scoped, full)

	if recordedDesired != clustersScopedDesired || recordedStructural != clustersScopedStructural {
		t.Errorf("a --clusters run would see a freshly-provisioned cluster as drifted: desired %s vs recorded %s, structural %s vs recorded %s", clustersScopedDesired, recordedDesired, clustersScopedStructural, recordedStructural)
	}
}
